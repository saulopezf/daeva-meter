package aion2

import "encoding/binary"

// enableEmbeddedDamage activa el escaneo embebido de 04 38 (§7.4). Está
// desactivado por defecto: el daño alineado a frame ya cubre el tráfico de
// combate del juego y la vía embebida corre el riesgo de contar dos veces.
// Actívalo si se observan golpes perdidos.
const enableEmbeddedDamage = false

// isMarker indica si en p[o:] empieza un "marcador": un byte entre 1 y 7 seguido
// de un cero. Marca el límite de un registro y se usa para no confundir el
// comienzo del siguiente campo con datos del actual.
func isMarker(p []byte, o int) bool {
	return o+1 < len(p) && p[o+1] == 0 && p[o] >= 1 && p[o] <= 7
}

// parseDamage parsea un paquete de daño 04 38. pkt es el frame completo
// (incluido el varint de longitud inicial) y pkt[off:off+2] es el opcode 04 38.
//
// El paquete puede encadenar varios registros de daño separados por 01 00. Cada
// registro se decodifica con esta estructura aproximada:
//
//	target(varint) selector(varint) flag(varint) actor(varint)
//	skill(u32 LE) uid(1) hitType(varint) ...campos según layout...
//
// El byte menos significativo de selector (layout) determina la disposición del
// resto del registro; el bit 0x20 de selector indica si hay golpes múltiples.
// Según el caso, la porción conocida se lee de forma estricta (parseHitTail) o
// con heurísticas para deducir el número y el daño de los golpes extra.
//
// allowEmbedded permite caer al escaneo embebido (§7.4) cuando el frame no
// empieza por 04 38. requireTrusted exige la combinación más fiable
// (actor != target y daño > 0) para aceptar el golpe.
//
// Devuelve true si se parseó al menos un golpe.
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
			off += 2 // separador entre registros encadenados
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
		if _, ok = tryVarint(pkt, &off); !ok { // flag (sin uso)
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
			skill = skill*10 + 1 // normaliza ids de habilidad "cortos"
		}
		if skill < 1 || skill > 299_999_999 {
			break
		}
		npcSkill := skill >= 1_000_000 && skill <= 9_999_999
		// Una habilidad de NPC solo se acepta cuando un mob ataca a un jugador
		// conocido: así se descarta daño recibido de otras fuentes.
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

		// Registro sin valor de daño: puede seguir siendo un golpe válido
		// (parado, esquivado...) si el tipo de impacto lo permite.
		if noValue {
			if !requireTrusted && actor != target && !npcSkill && (hitType == 1 || hitType == 6) {
				p.combat.AddHit(p.now, Hit{Target: target, Actor: actor, Skill: int(skill), HitType: hitType, NPCSkill: npcSkill})
			}
			break
		}

		// Longitud de los campos intermedios según el layout.
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

		// Cola estricta (§7.3): cuando el layout permite reconocer el bloque
		// final con certeza, se usa en lugar de las heurísticas.
		strict := false
		var tEnd, tField, tCount, tDmg int
		if (layout == 4 || layout == 6) && skill != 99_745_942 {
			tEnd, tField, tCount, tDmg, strict = parseHitTail(pkt, off, layout, sw, dmg)
		}
		// Heurística heredada, solo cuando la cola estricta no encajó.
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

		// Intenta leer los daños de golpes adicionales: una lista de cifras
		// parecidas, todas en rango plausible. allMatch/first permiten detectar
		// el caso "N golpes idénticos" cuando solo se anuncia el total.
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
		// El total anunciado (dmg) ya incluye los golpes extra: se descuentan
		// para no contarlos dos veces al guardarlos por separado.
		if multiCount > 0 && multiDmg > 0 && dmg > multiDmg {
			dmg -= multiDmg
		}

		// Curación opcional tras el bloque de daño (03 00 <cifra>).
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

// parseHitTail es el decodificador estricto de la cola del registro (§7.3).
// Prueba primero el layout indicado y, para el layout 4, reintenta como layout 6.
func parseHitTail(p []byte, off, layout, sw, value int) (end, field, count, dmg int, ok bool) {
	if e, f, c, d, ok := hitTailAs(p, off, layout, sw, value); ok {
		return e, f, c, d, true
	}
	if layout == 4 {
		return hitTailAs(p, off, 6, sw, value)
	}
	return 0, 0, 0, 0, false
}

// hitTailAs decodifica la cola con un layout concreto. Valida que el bloque
// resultante sea coherente (el daño de los golpes extra debe ser menor que el
// total) y que lo que queda tras el registro sea limpio: vacío, un marcador o el
// inicio del siguiente 04 38. Devuelve ok=false si algo no cuadra.
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

// parseEmbeddedDamage busca registros 04 38 que no estén al principio del frame
// (§7.4). Está detrás de enableEmbeddedDamage. Para cada candidato construye un
// frame sintético con una longitud falsa y lo parsea en modo "trusted",
// evitando repetir registros con el caché seenDamage.
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

// damageKey extrae una clave identificativa de un registro 04 38 candidato: los
// primeros bytes a partir de su posición, con un tope de 64.
func damageKey(pkt []byte, i int) string {
	end := min(i+64, len(pkt))
	return string(pkt[i:end])
}
