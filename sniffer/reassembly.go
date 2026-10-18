package sniffer

import (
	"strconv"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/tcpassembly"
)

// streamFactory crea un sniffStream por cada sentido de un flujo TCP.
type streamFactory struct {
	s *Sniffer
}

// New se llama cuando aparece un flujo TCP nuevo: asigna un id incremental,
// clasifica su dirección y crea el stream correspondiente.
func (f *streamFactory) New(netFlow, tcpFlow gopacket.Flow) tcpassembly.Stream {
	n := f.s.streams.Add(1)
	dir := flowDirection(tcpFlow, f.s.opts.Port)
	f.s.logf("stream #%d %s %s:%s -> %s:%s",
		n, dir, netFlow.Src(), tcpFlow.Src(), netFlow.Dst(), tcpFlow.Dst())
	return &sniffStream{s: f.s, id: n, dir: dir}
}

// flowDirection clasifica un flujo TCP según qué extremo usa el puerto del juego.
func flowDirection(tcpFlow gopacket.Flow, port uint16) Direction {
	p := strconv.FormatUint(uint64(port), 10)
	switch {
	case tcpFlow.Dst().String() == p:
		return ClientToServer
	case tcpFlow.Src().String() == p:
		return ServerToClient
	default:
		return UnknownDir
	}
}

// sniffStream recibe los bytes de payload reensamblados de un sentido de un flujo
// TCP. Los callbacks se ejecutan de forma síncrona dentro de las llamadas al
// Assembler, así que ya están serializados bajo Sniffer.mu.
type sniffStream struct {
	s          *Sniffer
	id         int64
	dir        Direction
	preview    bool
	gapped     bool
	classified bool
	ignore     bool
}

// Reassembled entrega los segmentos ya reensamblados de este stream. Avisa de
// huecos (segmentos perdidos o captura empezada a mitad), decide una sola vez por
// conexión si es TLS y, si no lo es, reenvía los bytes al callback OnPayload.
func (st *sniffStream) Reassembled(segs []tcpassembly.Reassembly) {
	for _, seg := range segs {
		if seg.Skip != 0 {
			st.s.gaps.Add(1)
			if !st.gapped {
				st.gapped = true
				st.s.logf("stream #%d: missing %d byte(s) before this segment (capture started mid-stream or packet lost)", st.id, seg.Skip)
			}
		}
		if len(seg.Bytes) == 0 {
			continue
		}
		// El servidor también habla TLS en el mismo puerto, en una conexión
		// aparte. Se decide una vez por conexión con su primer payload.
		if !st.classified {
			st.classified = true
			if looksLikeTLSRecord(seg.Bytes) {
				st.ignore = true
				st.s.logf("stream #%d: TLS connection detected, ignoring", st.id)
			}
		}
		if st.ignore {
			continue
		}
		st.s.payloadBytes.Add(int64(len(seg.Bytes)))
		if !st.preview {
			st.preview = true
			n := previewLen
			if len(seg.Bytes) < n {
				n = len(seg.Bytes)
			}
			st.s.logf("stream #%d first payload (%d of %d bytes): % x", st.id, n, len(seg.Bytes), seg.Bytes[:n])
		}
		if st.s.opts.OnPayload != nil {
			ts := seg.Seen
			if ts.IsZero() {
				ts = time.Now()
			}
			st.s.opts.OnPayload(st.id, st.dir, ts, seg.Bytes)
		}
	}
}

// looksLikeTLSRecord indica si b empieza con una cabecera de registro TLS
// plausible (tipo 0x14..0x17, versión 0x03 0x01..0x04, longitud 1..16640).
func looksLikeTLSRecord(b []byte) bool {
	if len(b) < 5 {
		return false
	}
	if b[0] < 0x14 || b[0] > 0x17 || b[1] != 0x03 || b[2] < 0x01 || b[2] > 0x04 {
		return false
	}
	l := int(b[3])<<8 | int(b[4])
	return l > 0 && l <= 16384+256
}

// ReassemblyComplete se llama cuando el flujo TCP termina.
func (st *sniffStream) ReassemblyComplete() {
	st.s.logf("stream #%d closed", st.id)
}
