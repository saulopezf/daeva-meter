package aion2

import (
	"encoding/binary"
	"testing"
	"time"

	"github.com/pierrec/lz4/v4"
)

// buildIdentityFrame construye un frame de identidad sintético (33 36 para self,
// 45 36 para otro jugador) con los campos opcionales que correspondan.
func buildIdentityFrame(t *testing.T, id int, name string, self bool) []byte {
	t.Helper()
	op := []byte{0x45, 0x36}
	if self {
		op = []byte{0x33, 0x36}
	}
	body := append([]byte{}, op...)
	body = append(body, encodeVarint(id)...)
	body = append(body, 0x11, 0x22, 0x33, 0x44) // mask1
	body = append(body, 0x01)                   // mask2: nombre presente
	body = append(body, byte(len(name)))
	body = append(body, []byte(name)...)
	if self {
		var srv [2]byte
		binary.LittleEndian.PutUint16(srv[:], 2306)
		body = append(body, srv[:]...)
		var cls [4]byte
		binary.LittleEndian.PutUint32(cls[:], 11)
		body = append(body, cls[:]...)
		body = append(body, 0x02)
		var lvl [4]byte
		binary.LittleEndian.PutUint32(lvl[:], 45)
		body = append(body, lvl[:]...)
	}
	L := len(body) + 4
	return append(encodeVarint(L), body...)
}

// buildBundleFrame comprime inner y lo envuelve en un frame de bundle
// (FF FF <tamaño LE> <LZ4>).
func buildBundleFrame(t *testing.T, inner []byte) []byte {
	t.Helper()
	buf := make([]byte, lz4.CompressBlockBound(len(inner)))
	n, err := lz4.CompressBlock(inner, buf, nil)
	if err != nil {
		t.Fatalf("compress: %v", err)
	}
	payload := []byte{0xFF, 0xFF}
	var sz [4]byte
	binary.LittleEndian.PutUint32(sz[:], uint32(len(inner)))
	payload = append(payload, sz[:]...)
	payload = append(payload, buf[:n]...)
	L := len(payload) + 4
	return append(encodeVarint(L), payload...)
}

// TestVarintRoundTrip comprueba que codificar y decodificar varints es estable en
// los bordes de anchura.
func TestVarintRoundTrip(t *testing.T) {
	for _, v := range []int{0, 1, 5, 63, 64, 127, 128, 300, 16383, 16384, 1 << 20, 1<<31 - 1} {
		enc := encodeVarint(v)
		got, n := readVarint(enc, 0)
		if n != len(enc) || int(got) != v {
			t.Errorf("varint %d: got (%d,%d) enc=%x", v, got, n, enc)
		}
	}
}

// TestFrameSizeAroundVarintWidth comprueba que un frame cuya longitud necesita un
// varint de 2 bytes no se dimensiona mal.
func TestFrameSizeAroundVarintWidth(t *testing.T) {
	body := make([]byte, 130) // frame total = varint de 1..2 bytes + body
	body[0], body[1] = 0x06, 0x38
	frame := append(encodeVarint(len(body)+4), body...)
	frames, consumed := walk(frame)
	if len(frames) != 1 || consumed != len(frame) {
		t.Fatalf("walk: frames=%d consumed=%d len=%d", len(frames), consumed, len(frame))
	}
	if frames[0].Bundle {
		t.Fatalf("did not expect a bundle")
	}
}

// TestWalkSkipsTLSAndPadding comprueba que walk ignora el relleno 0x00 y los
// registros TLS y aun así encuentra la identidad que va detrás.
func TestWalkSkipsTLSAndPadding(t *testing.T) {
	inner := buildIdentityFrame(t, 1234, "Tester", true)
	stream := []byte{0x00, 0x00}
	// Cabecera de registro TLS (tipo 0x16, versión 0x0303) con 8 bytes de payload.
	tls := []byte{0x16, 0x03, 0x03, 0x00, 0x08, 1, 2, 3, 4, 5, 6, 7, 8}
	stream = append(stream, tls...)
	stream = append(stream, inner...)
	stream = append(stream, 0x00)

	c := NewCombat()
	p := NewParser(c)
	if got := p.ConsumeStream(stream, time.Now()); got != len(stream) {
		t.Fatalf("consumed=%d, want %d", got, len(stream))
	}
	if e := c.entities[1234]; e == nil || e.Name != "Tester" {
		t.Fatalf("identity not parsed, entities=%v", c.entities)
	}
}

// TestChunkedStream verifica que un stream partido en trozos de distinto tamaño
// se reensambla y parsea igual que si llegara de una vez.
func TestChunkedStream(t *testing.T) {
	var stream []byte
	stream = append(stream, buildIdentityFrame(t, 1395, "Alpha", false)...)
	stream = append(stream, buildDamageFrame(t, "b1ea011600f30a40c0f40063028000010b199b5f01000000ac52d007")...)
	stream = append(stream, buildIdentityFrame(t, 222, "Beta", false)...)

	for _, chunk := range []int{1, 7, 100, 1460} {
		c := NewCombat()
		p := NewParser(c)
		for off := 0; off < len(stream); off += chunk {
			end := off + chunk
			if end > len(stream) {
				end = len(stream)
			}
			p.Feed(1, stream[off:end], time.Now())
		}
		snap := c.Snapshot(time.Now())
		if snap.Members == nil || len(snap.Members) != 1 {
			t.Fatalf("chunk=%d: members=%v", chunk, snap.Members)
		}
		m := snap.Members[0]
		if m.Name != "Alpha" || m.Damage != 976 {
			t.Fatalf("chunk=%d: got %+v", chunk, m)
		}
	}
}

// TestBundleUnwrap verifica que un frame de bundle se descomprime y que la
// identidad que contiene se parsea.
func TestBundleUnwrap(t *testing.T) {
	inner := buildIdentityFrame(t, 9999, "Bundled", true)
	bundle := buildBundleFrame(t, inner)

	c := NewCombat()
	p := NewParser(c)
	if got := p.ConsumeStream(bundle, time.Now()); got != len(bundle) {
		t.Fatalf("consumed=%d want %d", got, len(bundle))
	}
	e := c.entities[9999]
	if e == nil || e.Name != "Bundled" || e.Class != 11 || e.Level != 45 {
		t.Fatalf("bundle identity not parsed: %+v", e)
	}
}
