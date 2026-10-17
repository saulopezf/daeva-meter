package aion2

// readVarint reads an unsigned LEB128 varint (max 32 bits) at b[off:].
// It returns the value and the number of bytes read. bytesRead is -1 when the
// varint is invalid (>32 bits) or incomplete (buffer ends mid-varint).
func readVarint(b []byte, off int) (int32, int) {
	var v uint32
	shift, n := 0, 0
	for {
		if off+n >= len(b) {
			return -1, -1
		}
		c := b[off+n]
		n++
		v |= uint32(c&0x7F) << shift
		if c&0x80 == 0 {
			return int32(v), n
		}
		shift += 7
		if shift >= 32 {
			return -1, -1
		}
	}
}

// tryVarint reads a varint at *off, advancing it. It fails (and does not
// advance) when the varint is invalid or negative.
func tryVarint(b []byte, off *int) (int, bool) {
	v, n := readVarint(b, *off)
	if n <= 0 {
		return 0, false
	}
	*off += n
	if v < 0 {
		return 0, false
	}
	return int(v), true
}
