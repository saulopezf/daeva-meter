package aion2

import (
	"encoding/binary"

	"github.com/pierrec/lz4/v4"
)

// Límites de seguridad del framing. Protegen al parser de longitudes corruptas
// o maliciosas que podrían provocar reservas de memoria desproporcionadas.
const (
	maxPacket       = 65535     // tamaño máximo de un frame
	maxFragmentWait = 16384     // espera máxima para completar un frame normal
	maxBundleWait   = 65535     // espera máxima cuando el frame parece un bundle
	maxDecompressed = 1_000_000 // tamaño máximo de un bundle ya descomprimido
	maxBundleDepth  = 4         // anidamiento máximo de bundles
)

// Frame es una unidad ya enmarcada dentro del stream servidor->cliente.
// PayloadStart es el desplazamiento del payload (el opcode) dentro del frame, es
// decir cuántos bytes ocupa el varint de longitud inicial.
type Frame struct {
	Start        int
	End          int
	PayloadStart int
	Bundle       bool
}

// tlsRecordSize devuelve el tamaño total de un registro TLS al comienzo de b, o
// false si b no empieza con una cabecera TLS plausible. El servidor usa TLS en
// el mismo puerto para conexiones que no son de juego, así que el framing debe
// saltarse esos registros para no confundirlos con frames.
func tlsRecordSize(b []byte) (int, bool) {
	if len(b) < 5 || b[0] < 0x14 || b[0] > 0x17 || b[1] != 0x03 || b[2] < 0x01 || b[2] > 0x04 {
		return 0, false
	}
	l := int(binary.BigEndian.Uint16(b[3:]))
	return 5 + l, l > 0 && l <= 16384+256
}

// bundleHeaderAt indica si en b[at:] hay una cabecera de bundle válida
// (magia FF FF seguida del tamaño sin comprimir, little-endian). Se usa para
// decidir cuánto esperar antes de tratar un frame incompleto como fragmento.
func bundleHeaderAt(b []byte, at int) bool {
	if at+6 > len(b) {
		return false
	}
	sz := binary.LittleEndian.Uint32(b[at+2:])
	return b[at] == 0xFF && b[at+1] == 0xFF && sz > 0 && sz <= maxDecompressed
}

// walk recorre el stream exterior servidor->cliente y delimita frames.
//
// Estrategia: va leyendo el varint de longitud de cada frame (longitud total =
// L - 4 + n, donde n son los bytes del varint) y comprueba su coherencia. Como
// el stream puede empezar en mitad de una transmisión o contener basura,
// resincroniza avanzando byte a byte cuando encuentra un tamaño imposible. Si el
// frame está bien formado pero aún no han llegado todos sus bytes, se detiene:
// es un fragmento legítimo y hay que esperar más datos (limitado por
// maxFragmentWait/maxBundleWait).
//
// Devuelve los frames hallados y el número de bytes consumidos con seguridad
// (todo lo anterior al primer frame incompleto).
func walk(b []byte) (frames []Frame, consumed int) {
	off := 0
	for off < len(b) {
		if b[off] == 0x00 { // relleno
			off++
			continue
		}
		if sz, ok := tlsRecordSize(b[off:]); ok {
			if off+sz > len(b) {
				break
			}
			off += sz
			continue
		}
		L, n := readVarint(b, off)
		if n <= 0 || L <= 0 {
			if off+5 > len(b) {
				break
			}
			off++ // byte de basura: resincroniza
			continue
		}
		size := int(L) - 4 + n
		if size < n || size <= 0 {
			off++
			continue
		}
		if size > maxPacket {
			off++
			continue
		}
		if off+size > len(b) {
			limit := maxFragmentWait
			if bundleHeaderAt(b, off+n) {
				limit = maxBundleWait
			}
			if size > limit {
				off++ // tamaño irreal para un fragmento: probable basura
				continue
			}
			break // fragmento legítimo: espera a recibir más datos
		}
		isBundle := n+1 < size && b[off+n] == 0xFF && b[off+n+1] == 0xFF
		frames = append(frames, Frame{Start: off, End: off + size, PayloadStart: n, Bundle: isBundle})
		off += size
	}
	return frames, off
}

// walkInner recorre el contenido ya descomprimido de un bundle. A diferencia de
// walk, nunca resincroniza: al primer indicio de problema se detiene, porque el
// contenido sano de un bundle siempre está perfectamente enmarcado.
func walkInner(b []byte) []Frame {
	var frames []Frame
	off := 0
	for off < len(b) {
		if b[off] == 0x00 {
			off++
			continue
		}
		L, n := readVarint(b, off)
		if n <= 0 || L <= 0 {
			break
		}
		size := int(L) - 4 + n
		if size < n || size <= 0 || size > maxPacket {
			break
		}
		if off+size > len(b) {
			break
		}
		isBundle := n+1 < size && b[off+n] == 0xFF && b[off+n+1] == 0xFF
		frames = append(frames, Frame{Start: off, End: off + size, PayloadStart: n, Bundle: isBundle})
		off += size
	}
	return frames
}

// decompressBundle infla el payload de un bundle, que empieza con la magia:
//
//	FF FF <u32 LE tamaño sin comprimir> <bloque LZ4 crudo>
func decompressBundle(payload []byte) ([]byte, bool) {
	if len(payload) < 7 {
		return nil, false
	}
	size := binary.LittleEndian.Uint32(payload[2:6])
	if size == 0 || size > maxDecompressed {
		return nil, false
	}
	dst := make([]byte, size)
	n, err := lz4.UncompressBlock(payload[6:], dst)
	if err != nil {
		return nil, false
	}
	return dst[:n], true
}

// regions devuelve los rangos de bytes que hay que escanear buscando registros
// embebidos: los huecos entre frames, los propios frames que no son bundles y
// los bytes finales sobrantes. Los frames de bundle se excluyen porque su
// contenido se escanea tras descomprimirlo.
func regions(frames []Frame, end int) [][2]int {
	var out [][2]int
	at := 0
	for _, f := range frames {
		if f.Start > at {
			out = append(out, [2]int{at, f.Start})
		}
		if !f.Bundle {
			out = append(out, [2]int{f.Start, f.End})
		}
		at = f.End
	}
	if end > at {
		out = append(out, [2]int{at, end})
	}
	return out
}
