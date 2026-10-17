package aion2

import (
	"encoding/binary"
	"unicode"
	"unicode/utf8"
)

// opIdentitySelf is your own character record; opIdentityOther is another
// player (pre-June 2026 it was 44 36).
var (
	opIdentitySelf  = [2]byte{0x33, 0x36}
	opIdentityOther = [2]byte{0x45, 0x36}
)

// exactName validates the name field of an identity/spawn record: 1..=48 bytes
// of valid UTF-8, 1..=12 alphanumeric characters (any alphabet) with at least
// one letter. The whole field must be the name.
func exactName(b []byte, off, ln int) (string, bool) {
	if ln < 1 || ln > 48 || off+ln > len(b) {
		return "", false
	}
	raw := b[off : off+ln]
	if !utf8.Valid(raw) {
		return "", false
	}
	runes := []rune(string(raw))
	if len(runes) < 1 || len(runes) > 12 {
		return "", false
	}
	hasLetter := false
	for _, c := range runes {
		if !unicode.IsLetter(c) && !unicode.IsDigit(c) {
			return "", false
		}
		if unicode.IsLetter(c) {
			hasLetter = true
		}
	}
	if !hasLetter {
		return "", false
	}
	return string(runes), true
}

// isTutorialName detects the "$abcd" tutorial character placeholder.
func isTutorialName(b []byte, off, ln int) bool {
	if ln < 5 || off+ln > len(b) || b[off] != '$' {
		return false
	}
	for _, c := range b[off+1 : off+ln] {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')) {
			return false
		}
	}
	return true
}

// parseIdentityAt parses an identity record whose opcode sits at b[i:i+2].
func (p *Parser) parseIdentityAt(b []byte, i int, self bool) bool {
	if i+3 >= len(b) {
		return false
	}
	o := i + 2
	id, ok := tryVarint(b, &o)
	if !ok || id <= 0 || id > 9_999_999 {
		return false
	}
	if o+5 > len(b) {
		return false
	}
	o += 4 // mask1
	mask2 := b[o]
	o++
	name := ""
	if mask2&0x01 != 0 {
		if o >= len(b) {
			return false
		}
		ln := int(b[o])
		o++
		if isTutorialName(b, o, ln) {
			name = ""
		} else {
			nm, ok := exactName(b, o, ln)
			if !ok {
				return false
			}
			name = nm
			o += ln
		}
	}
	if self {
		if o+2 > len(b) {
			return false
		}
		server := int(binary.LittleEndian.Uint16(b[o:]))
		o += 2
		if server < 1000 || server > 2999 {
			return false
		}
		if o+5 > len(b) {
			return false
		}
		class := int(binary.LittleEndian.Uint32(b[o:]))
		o += 4
		o++ // 1 unknown byte
		level := 0
		if o+4 <= len(b) {
			level = int(binary.LittleEndian.Uint32(b[o:]))
		}
		p.combat.SetIdentity(id, name, true, server, class, level)
		return true
	}
	p.combat.SetIdentity(id, name, false, 0, 0, 0)
	return true
}

// scanIdentity searches a byte region for identity records that are not at a
// frame boundary (they also arrive embedded inside other packets).
func (p *Parser) scanIdentity(b []byte, regs [][2]int) {
	for _, r := range regs {
		for i := r[0]; i+2 < r[1]; i++ {
			switch {
			case b[i] == opIdentitySelf[0] && b[i+1] == opIdentitySelf[1]:
				p.parseIdentityAt(b, i, true)
			case b[i] == opIdentityOther[0] && b[i+1] == opIdentityOther[1]:
				p.parseIdentityAt(b, i, false)
			}
		}
	}
}
