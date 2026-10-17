package aion2

import (
	"unicode/utf8"
)

// rosterMember is one member extracted from a party roster record (02 97).
type rosterMember struct {
	Name  string
	Class int
	Level int
}

// parseRosterAt parses a party roster record (spec §8.6) whose opcode sits at
// b[i:i+2]. The header is validated strictly, which filters out accidental
// 02 97 byte pairs; member entries are then walked with the documented layout
// and the variable-length tail is bridged by searching for the next slot.
//
// Returns ok=false when the header or the first member does not validate.
func parseRosterAt(b []byte, i int) (members []rosterMember, ok bool) {
	o := i + 2
	if o+4 > len(b) {
		return nil, false
	}
	o += 4 // party_key u32
	if o >= len(b) {
		return nil, false
	}
	nameLen := int(b[o])
	o++
	if nameLen < 1 || nameLen > 40 || o+nameLen > len(b) {
		return nil, false
	}
	if !validRosterText(b[o : o+nameLen]) {
		return nil, false
	}
	o += nameLen

	if o+5 > len(b) {
		return nil, false
	}
	partySize := int(b[o])
	o++
	if partySize < 1 || partySize > 12 {
		return nil, false
	}
	o += 4 // dungeon_id u32
	o += 2
	if o+8 > len(b) {
		return nil, false
	}
	o += 8 // leader_dbid u64
	o += 3

	count, ok := varintInRange(b, &o, 1, 12)
	if !ok {
		return nil, false
	}

	prevSlot := 0
	for m := 0; m < count && o+11 <= len(b); m++ {
		slot := int(b[o+1])
		server := int(b[o+8]) | int(b[o+9])<<8
		nickLen := int(b[o+10])
		if slot < 1 || slot > 12 || server < 1000 || server > 2999 ||
			nickLen < 1 || nickLen > 40 || o+11+nickLen > len(b) {
			break
		}
		name, okName := exactName(b, o+11, nickLen)
		if !okName {
			break
		}
		o += 11 + nickLen

		class, level := 0, 0
		if o+8 <= len(b) {
			class = int(le32(b[o:]))
			level = int(le32(b[o+4:]))
			o += 8 // clase u32, nivel u32
		}
		members = append(members, rosterMember{Name: name, Class: class, Level: level})
		prevSlot = slot

		if m == count-1 {
			break
		}
		next, okNext := findNextMember(b, o, prevSlot)
		if !okNext {
			break
		}
		o = next
	}
	if len(members) == 0 {
		return nil, false
	}
	return members, true
}

// findNextMember searches the variable-length tail of a roster member for the
// start of the next one: next slot number, a plausible server and a valid
// nickname.
func findNextMember(b []byte, from, prevSlot int) (int, bool) {
	end := min(from+64, len(b)-11)
	for p := max(from, 0); p < end; p++ {
		if int(b[p+1]) != prevSlot+1 {
			continue
		}
		server := int(b[p+8]) | int(b[p+9])<<8
		if server < 1000 || server > 2999 {
			continue
		}
		nickLen := int(b[p+10])
		if nickLen < 1 || nickLen > 40 || p+11+nickLen > len(b) {
			continue
		}
		if _, ok := exactName(b, p+11, nickLen); ok {
			return p, true
		}
	}
	return 0, false
}

// validRosterText accepts a short UTF-8 string with no control characters.
func validRosterText(b []byte) bool {
	if len(b) == 0 || !utf8.Valid(b) {
		return false
	}
	for _, r := range string(b) {
		if r < 0x20 {
			return false
		}
	}
	return true
}

func le32(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}

// varintInRange reads a varint at *off and requires it to fall in [lo, hi].
func varintInRange(b []byte, off *int, lo, hi int) (int, bool) {
	v, ok := tryVarint(b, off)
	if !ok || v < lo || v > hi {
		return 0, false
	}
	return v, true
}

// scanRoster searches byte regions for party roster records and feeds them to
// the combat model.
func (p *Parser) scanRoster(b []byte, regs [][2]int) {
	for _, r := range regs {
		for i := r[0]; i+2 < r[1]; i++ {
			if b[i] != 0x02 || b[i+1] != 0x97 {
				continue
			}
			if members, ok := parseRosterAt(b, i); ok {
				p.combat.SetParty(members)
			}
		}
	}
}
