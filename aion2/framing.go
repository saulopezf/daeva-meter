package aion2

import (
	"encoding/binary"

	"github.com/pierrec/lz4/v4"
)

const (
	maxPacket       = 65535
	maxFragmentWait = 16384
	maxBundleWait   = 65535
	maxDecompressed = 1_000_000
	maxBundleDepth  = 4
)

// Frame is one framed unit inside the server->client byte stream. PayloadStart
// is the offset of the payload (opcode) inside the frame, i.e. the number of
// bytes the leading varint occupies.
type Frame struct {
	Start        int
	End          int
	PayloadStart int
	Bundle       bool
}

// tlsRecordSize returns the total size of a TLS record at the start of b, or
// false if b does not begin with a plausible TLS record header.
func tlsRecordSize(b []byte) (int, bool) {
	if len(b) < 5 || b[0] < 0x14 || b[0] > 0x17 || b[1] != 0x03 || b[2] < 0x01 || b[2] > 0x04 {
		return 0, false
	}
	l := int(binary.BigEndian.Uint16(b[3:]))
	return 5 + l, l > 0 && l <= 16384+256
}

func bundleHeaderAt(b []byte, at int) bool {
	if at+6 > len(b) {
		return false
	}
	sz := binary.LittleEndian.Uint32(b[at+2:])
	return b[at] == 0xFF && b[at+1] == 0xFF && sz > 0 && sz <= maxDecompressed
}

// walk scans the outer server->client stream, resynchronising byte by byte
// when it finds garbage or a mis-sized length. It returns the frames found and
// the number of bytes safely consumed (everything before the first incomplete
// frame).
func walk(b []byte) (frames []Frame, consumed int) {
	off := 0
	for off < len(b) {
		if b[off] == 0x00 {
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
			off++
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
				off++
				continue
			}
			break // legitimate fragment: wait for more data
		}
		isBundle := n+1 < size && b[off+n] == 0xFF && b[off+n+1] == 0xFF
		frames = append(frames, Frame{Start: off, End: off + size, PayloadStart: n, Bundle: isBundle})
		off += size
	}
	return frames, off
}

// walkInner scans the decompressed content of a bundle. It never
// resynchronises: the first sign of trouble stops the scan.
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

// decompressBundle inflates a bundle payload that starts at the FF FF magic:
// FF FF <u32 LE uncompressed size> <raw LZ4 block>.
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

// regions returns the byte ranges to scan for embedded records: the gaps
// between frames, the non-bundle frames themselves, and any trailing bytes.
// Bundle frames are excluded because their content is scanned after
// decompression.
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
