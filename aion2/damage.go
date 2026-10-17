package aion2

import "encoding/binary"

// enableEmbeddedDamage gates the 04 38 embedded scan (§7.4). It is off by
// default: frame-aligned damage already covers the game's combat traffic, and
// the embedded path risks double counting. Flip it on if hits go missing.
const enableEmbeddedDamage = false

func isMarker(p []byte, o int) bool {
	return o+1 < len(p) && p[o+1] == 0 && p[o] >= 1 && p[o] <= 7
}

// parseDamage parses one 04 38 damage packet. pkt is the whole frame (leading
// varint included) and pkt[off:off+2] is the 04 38 opcode.
func (p *Parser) parseDamage(pkt []byte, allowEmbedded, requireTrusted bool) bool {
	_, n := readVarint(pkt, 0)
	if n < 0 || n+1 >= len(pkt) {
		return false
	}
	if pkt[n] != 0x04 || pkt[n+1] != 0x38 {
		if allowEmbedded {
			return p.parseEmbeddedDamage(pkt)
		}
		return false
	}
	off := n + 2
	parsedAny := false
	for off < len(pkt) {
		chained := false
		if off+1 < len(pkt) && pkt[off] == 0x01 && pkt[off+1] == 0x00 {
			off += 2
			chained = true
		}
		if parsedAny && !chained {
			break
		}

		target, ok := tryVarint(pkt, &off)
		if !ok || !p.plausibleID(target) {
			break
		}
		sw, ok := tryVarint(pkt, &off)
		if !ok {
			break
		}
		layout := sw & 0x0F
		noValue := layout == 0 || layout == 2
		if (layout < 4 || layout > 7) && !noValue {
			break
		}
		if _, ok = tryVarint(pkt, &off); !ok { // flag (unused)
			break
		}
		actor, ok := tryVarint(pkt, &off)
		if !ok || !p.plausibleID(actor) {
			break
		}
		if off+4 > len(pkt) {
			break
		}
		skill := int64(binary.LittleEndian.Uint32(pkt[off:]))
		off += 4
		if skill >= 3_000_000 && skill <= 3_099_999 {
			skill = skill*10 + 1
		}
		if skill < 1 || skill > 299_999_999 {
			break
		}
		npcSkill := skill >= 1_000_000 && skill <= 9_999_999
		if npcSkill && !(p.combat.isMob(actor) && p.combat.isKnownPlayer(target)) {
			break
		}
		if off < len(pkt) {
			off++ // uid
		}
		hitType, ok := tryVarint(pkt, &off)
		if !ok {
			break
		}

		if noValue {
			if !requireTrusted && actor != target && !npcSkill && (hitType == 1 || hitType == 6) {
				p.combat.AddHit(p.now, Hit{Target: target, Actor: actor, Skill: int(skill), HitType: hitType, NPCSkill: npcSkill})
			}
			break
		}

		skip := map[int]int{5: 12, 6: 10, 7: 14}[layout]
		if skip == 0 {
			skip = 8
		}
		var flags, angle byte
		hasAngle := false
		plotterExtra := 0
		if layout&0x02 != 0 {
			if off >= len(pkt) {
				break
			}
			flags = pkt[off]
			at := off + 1
			if _, ok := tryVarint(pkt, &at); !ok {
				break
			}
			plotterExtra = (at - off - 1) - 1
			if at < len(pkt) {
				angle, hasAngle = pkt[at], true
			}
		}
		off += skip + plotterExtra
		if off >= len(pkt) {
			break
		}

		v1, ok := tryVarint(pkt, &off)
		if !ok {
			break
		}
		v2, ok := tryVarint(pkt, &off)
		if !ok {
			break
		}
		if v1 == 0 {
			if v3, ok := tryVarint(pkt, &off); ok {
				v1, v2 = v2, v3
			}
		}
		dmg := v2

		// Strict tail (§7.3).
		strict := false
		var tEnd, tField, tCount, tDmg int
		if (layout == 4 || layout == 6) && skill != 99_745_942 {
			tEnd, tField, tCount, tDmg, strict = parseHitTail(pkt, off, layout, sw, dmg)
		}
		// Legacy heuristic, only when the strict tail did not fit.
		hitCount := 0
		if !strict && sw&0x30 == 0x30 && off < len(pkt) {
			tryVarint(pkt, &off)
		}
		pre := off
		if !strict && off < len(pkt) && !isMarker(pkt, off) {
			if v, ok := tryVarint(pkt, &off); ok {
				if v <= 25 {
					hitCount = v
				} else if !isMarker(pkt, off) {
					if v2b, ok := tryVarint(pkt, &off); ok {
						if v2b <= 25 {
							hitCount = v2b
						} else {
							off = pre
						}
					}
				}
			}
		}
		if dmg < 0 || dmg > 99_999_999 {
			break
		}

		multiCount, multiDmg, first, haveFirst, allMatch := 0, 0, 0, false, true
		if !strict && hitCount > 0 && off < len(pkt) {
			capV := max(dmg, 500_000)
			read := 0
			for read < min(hitCount, 25) && off < len(pkt) {
				if isMarker(pkt, off) || (off+1 < len(pkt) && pkt[off] == 0x04 && pkt[off+1] == 0x38) {
					break
				}
				h, ok := tryVarint(pkt, &off)
				if !ok {
					break
				}
				if h > capV || h < 50 {
					multiDmg, haveFirst, allMatch = 0, false, true
					break
				}
				if !haveFirst {
					first, haveFirst = h, true
				} else if h != first {
					allMatch = false
				}
				multiDmg += h
				read++
			}
			multiCount = read
		}
		if !strict && sw == 54 && hitCount > multiCount && multiCount == 1 && haveFirst && allMatch {
			multiCount, multiDmg = hitCount, first*hitCount
		}
		if !strict && sw == 54 && multiCount > 0 && haveFirst && allMatch &&
			v2-multiCount*first <= first && v2/10 == first {
			dmg = first
		}
		if strict {
			off, hitCount, multiCount, multiDmg = tEnd, tField, tCount, tDmg
		}
		if multiCount > 0 && multiDmg > 0 && dmg > multiDmg {
			dmg -= multiDmg
		}

		heal := 0
		if off+1 < len(pkt) && pkt[off] == 0x03 && pkt[off+1] == 0x00 {
			off += 2
			if h, ok := tryVarint(pkt, &off); ok && h > 0 && h < 10_000_000 {
				heal = h
			}
		}

		if requireTrusted && !(actor != target && hitType >= 1 && hitType <= 3 && dmg > 0) {
			break
		}

		p.combat.AddHit(p.now, Hit{
			Target:      target,
			Actor:       actor,
			Skill:       int(skill),
			HitType:     hitType,
			Crit:        hitType == 3,
			Damage:      dmg,
			ExtraHits:   multiCount,
			ExtraDamage: multiDmg,
			Heal:        heal,
			PowerScalar: v1,
			NPCSkill:    npcSkill,
		})
		_ = flags
		_ = angle
		_ = hasAngle
		parsedAny = true
	}
	return parsedAny
}

// parseHitTail is the strict tail decoder (§7.3).
func parseHitTail(p []byte, off, layout, sw, value int) (end, field, count, dmg int, ok bool) {
	if e, f, c, d, ok := hitTailAs(p, off, layout, sw, value); ok {
		return e, f, c, d, true
	}
	if layout == 4 {
		return hitTailAs(p, off, 6, sw, value)
	}
	return 0, 0, 0, 0, false
}

func hitTailAs(p []byte, off, layout, sw, value int) (end, field, count, dmg int, ok bool) {
	if layout == 4 {
		v, got := tryVarint(p, &off)
		if !got || v < 1 || v > 25 {
			return 0, 0, 0, 0, false
		}
		field = v
	}
	if sw&0x20 != 0 {
		c, got := tryVarint(p, &off)
		if !got || c < 1 || c > 25 {
			return 0, 0, 0, 0, false
		}
		count = c
		for i := 0; i < c; i++ {
			h, got := tryVarint(p, &off)
			if !got {
				return 0, 0, 0, 0, false
			}
			dmg += h
		}
		if dmg >= value {
			return 0, 0, 0, 0, false
		}
	}
	rest := p[min(off, len(p)):]
	clean := len(rest) == 0 ||
		(len(rest) >= 2 && rest[1] == 0 && rest[0] >= 1 && rest[0] <= 7) ||
		(len(rest) >= 2 && rest[0] == 0x04 && rest[1] == 0x38)
	return off, field, count, dmg, clean
}

// parseEmbeddedDamage finds 04 38 records that are not at the frame start
// (§7.4). Kept behind enableEmbeddedDamage.
func (p *Parser) parseEmbeddedDamage(pkt []byte) bool {
	parsed := false
	for i := 0; i+1 < len(pkt); i++ {
		if pkt[i] != 0x04 || pkt[i+1] != 0x38 {
			continue
		}
		key := damageKey(pkt, i)
		if p.damageSeen(key) {
			continue
		}
		fake := make([]byte, 0, len(pkt)-i+2)
		fake = append(fake, 0xFF, 0x01)
		fake = append(fake, pkt[i:]...)
		if p.parseDamage(fake, false, true) {
			p.markDamageSeen(key)
			parsed = true
		}
	}
	return parsed
}

func damageKey(pkt []byte, i int) string {
	end := min(i+64, len(pkt))
	return string(pkt[i:end])
}
