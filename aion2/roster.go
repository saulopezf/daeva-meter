package aion2

import (
	"unicode/utf8"
)

// rosterMember es un miembro extraído de un registro de roster de grupo (02 97).
type rosterMember struct {
	Name  string
	Class int
	Level int
}

// parseRosterAt parsea un registro de roster de grupo (spec §8.6) cuyo opcode
// está en b[i:i+2]. La cabecera se valida de forma estricta, lo que descarta
// parejas de bytes 02 97 accidentales; después se recorren las entradas de
// miembro con el layout documentado y se salva la cola de longitud variable
// buscando el siguiente slot.
//
// Devuelve ok=false cuando la cabecera o el primer miembro no validan.
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

// findNextMember busca en la cola de longitud variable de un miembro el inicio
// del siguiente: número de slot consecutivo, servidor plausible y nick válido.
// Es el puente que permite recorrer miembros pese a los campos de tamaño
// desconocido que van entre ellos.
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

// validRosterText acepta una cadena UTF-8 corta sin caracteres de control.
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

// le32 lee un entero de 32 bits little-endian de b.
func le32(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}

// varintInRange lee un varint en *off y exige que quede dentro de [lo, hi].
func varintInRange(b []byte, off *int, lo, hi int) (int, bool) {
	v, ok := tryVarint(b, off)
	if !ok || v < lo || v > hi {
		return 0, false
	}
	return v, true
}

// scanRoster busca registros de roster de grupo en las regiones de bytes y los
// entrega al modelo de combate.
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
