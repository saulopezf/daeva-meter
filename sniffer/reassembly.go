package sniffer

import (
	"strconv"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/tcpassembly"
)

// streamFactory creates one sniffStream per TCP direction.
type streamFactory struct {
	s *Sniffer
}

func (f *streamFactory) New(netFlow, tcpFlow gopacket.Flow) tcpassembly.Stream {
	n := f.s.streams.Add(1)
	dir := flowDirection(tcpFlow, f.s.opts.Port)
	f.s.logf("stream #%d %s %s:%s -> %s:%s",
		n, dir, netFlow.Src(), tcpFlow.Src(), netFlow.Dst(), tcpFlow.Dst())
	return &sniffStream{s: f.s, id: n, dir: dir}
}

// flowDirection classifies a TCP flow by which side uses the game port.
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

// sniffStream receives reassembled payload bytes for one TCP direction.
// Callbacks run synchronously inside Assembler calls, so they are already
// serialized under Sniffer.mu.
type sniffStream struct {
	s          *Sniffer
	id         int64
	dir        Direction
	preview    bool
	gapped     bool
	classified bool
	ignore     bool
}

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
		// The server also speaks TLS on the same port, in a separate
		// connection. Decide once per connection with its first payload.
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

// looksLikeTLSRecord reports whether b begins with a plausible TLS record
// header (type 0x14..0x17, version 0x03 0x01..0x04, length 1..16640).
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

func (st *sniffStream) ReassemblyComplete() {
	st.s.logf("stream #%d closed", st.id)
}
