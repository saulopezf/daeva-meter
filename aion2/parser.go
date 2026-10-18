package aion2

import (
	"encoding/binary"
	"sync"
	"time"
)

const (
	maxStreamBuf   = 2 << 20 // 2 MiB por stream antes de reiniciar el buffer
	damageLRUSlots = 16384   // entradas del caché de daños ya vistos
)

// Parser convierte un stream de bytes servidor->cliente en eventos de combate.
// Mantiene un buffer de reensamblado por cada stream TCP y alimenta un modelo
// Combat compartido.
type Parser struct {
	combat *Combat

	mu   sync.Mutex
	bufs map[int64][]byte
	now  time.Time

	// seenDamage/seenOrder forman un caché LRU de claves de daño ya procesadas
	// para no contar dos veces el mismo registro (ver parseEmbeddedDamage).
	seenDamage map[string]struct{}
	seenOrder  []string
	seenNext   int
}

// NewParser devuelve un parser que escribe en combat.
func NewParser(combat *Combat) *Parser {
	return &Parser{
		combat:     combat,
		bufs:       map[int64][]byte{},
		seenDamage: map[string]struct{}{},
		seenOrder:  make([]string, damageLRUSlots),
	}
}

// Feed añade bytes recién reensamblados de un stream y parsea tantos frames
// completos como sea posible. ts es el momento de captura de esos datos.
//
// Los bytes sobrantes (frame incompleto) se conservan en el buffer del stream
// para la siguiente llamada, de modo que un frame partido entre dos paquetes se
// reconstruye correctamente.
func (p *Parser) Feed(streamID int64, data []byte, ts time.Time) {
	if len(data) == 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	buf := append(p.bufs[streamID], data...)
	if len(buf) > maxStreamBuf {
		buf = buf[:0] // protección contra un stream que nunca se sincroniza
	}
	for len(buf) > 0 {
		consumed := p.consume(buf, ts)
		if consumed <= 0 {
			break
		}
		buf = buf[consumed:]
	}
	p.bufs[streamID] = buf
}

// ConsumeStream parsea un buffer en el sitio (lo usan los tests y el replay).
// Devuelve los bytes consumidos.
func (p *Parser) ConsumeStream(buf []byte, ts time.Time) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.consume(buf, ts)
}

// consume procesa un buffer: delimita los frames, resuelve los bundles, parsea
// cada paquete y después escanea las regiones en busca de registros embebidos.
func (p *Parser) consume(buf []byte, ts time.Time) int {
	p.now = ts
	frames, consumed := walk(buf)
	for _, f := range frames {
		if f.Bundle {
			p.unwrapBundle(buf[f.Start+f.PayloadStart:f.End], 1, ts)
			continue
		}
		pkt := buf[f.Start:f.End]
		p.parsePacket(pkt, ts)
	}
	p.scanRegion(buf, regions(frames, len(buf)))
	return consumed
}

// scanRegion ejecuta los escáneres de patrones (identidad, roster) sobre las
// regiones de bytes, y además abre los bundles embebidos dentro de paquetes
// (spec §6.2), por donde viajan copias de la identidad y del roster del grupo.
func (p *Parser) scanRegion(b []byte, regs [][2]int) {
	p.scanIdentity(b, regs)
	p.scanRoster(b, regs)
	p.scanEmbeddedBundles(b, regs, 1)
}

// scanEmbeddedBundles busca payloads de bundle (FF FF) dentro de paquetes cuya
// longitud inicial termina justo donde empieza el bundle, los descomprime y
// aplica solo los escáneres de identidad y de roster.
func (p *Parser) scanEmbeddedBundles(b []byte, regs [][2]int, depth int) {
	if depth > maxBundleDepth {
		return
	}
	for _, r := range regs {
		for i := r[0] + 1; i+1 < r[1]; i++ {
			if b[i] != 0xFF || b[i+1] != 0xFF {
				continue
			}
			if !p.tryEmbeddedBundle(b, i, depth) {
				continue
			}
			i += 1
		}
	}
}

// tryEmbeddedBundle intenta decodificar un bundle embebido que empieza en i (la
// magia FF FF). Devuelve true cuando el bundle se procesó correctamente.
//
// Un bundle embebido no está en un límite de frame, así que su longitud se
// localiza probando las tres anchuras posibles del varint que lo precede (1, 2 o
// 3 bytes) y quedándose con la que produce un final coherente.
func (p *Parser) tryEmbeddedBundle(b []byte, i, depth int) bool {
	for n := 3; n >= 1; n-- {
		at := i - n
		if at < 0 {
			continue
		}
		L, got := readVarint(b, at)
		if got != n || L <= 0 {
			continue
		}
		end := at + (int(L) - 4 + n)
		if end <= i || end > len(b) {
			continue
		}
		data, ok := decompressBundle(b[i:end])
		if !ok {
			continue
		}
		frames := walkInner(data)
		regs := regions(frames, len(data))
		p.scanIdentity(data, regs)
		p.scanRoster(data, regs)
		p.scanEmbeddedBundles(data, regs, depth+1)
		return true
	}
	return false
}

// unwrapBundle descomprime el payload de un bundle y procesa recursivamente su
// contenido: los paquetes normales van a parsePacket y las regiones resultantes
// a scanRegion, con el mismo tratamiento que el nivel exterior.
func (p *Parser) unwrapBundle(payload []byte, depth int, ts time.Time) {
	if depth > maxBundleDepth {
		return
	}
	data, ok := decompressBundle(payload)
	if !ok {
		return
	}
	frames := walkInner(data)
	for _, f := range frames {
		if f.Bundle {
			p.unwrapBundle(data[f.Start+f.PayloadStart:f.End], depth+1, ts)
			continue
		}
		p.parsePacket(data[f.Start:f.End], ts)
	}
	p.scanRegion(data, regions(frames, len(data)))
}

// parsePacket pasa cada parser sobre un frame; cada uno ignora lo que no sea su
// propio opcode. El opcode son los dos bytes que siguen al varint de longitud.
func (p *Parser) parsePacket(pkt []byte, ts time.Time) {
	_, n := readVarint(pkt, 0)
	if n <= 0 || n+1 >= len(pkt) {
		return
	}
	op := [2]byte{pkt[n], pkt[n+1]}
	switch op {
	case [2]byte{0x04, 0x38}: // daño / curación
		p.parseDamage(pkt, enableEmbeddedDamage, false)
	case opIdentitySelf:
		p.parseIdentityAt(pkt, n, true)
	case opIdentityOther:
		p.parseIdentityAt(pkt, n, false)
	case [2]byte{0x41, 0x36}: // aparición de summon / mob
		p.parseSummonSpawn(pkt, ts)
	case [2]byte{0x4A, 0x36}: // stats del jugador local: identifica a self
		p.parseSelfStats(pkt)
	case [2]byte{0x23, 0x36}: // teletransporte de la entidad 0 = cambio de zona
		p.parseZoneChange(pkt)
	case [2]byte{0x13, 0x92}: // salida del grupo
		p.parseGroupExit(pkt, 0x13, 0x92)
	case [2]byte{0x1D, 0x97}: // salida de la raid
		p.parseGroupExit(pkt, 0x1D, 0x97)
	}
}

// parseSelfStats maneja 4A 36 <id> (§8.8): las stats del jugador local, que
// permiten identificar el id de nuestra propia entidad incluso sin haber visto
// un registro de identidad 33 36.
func (p *Parser) parseSelfStats(pkt []byte) {
	_, n := readVarint(pkt, 0)
	if n <= 0 || n+1 >= len(pkt) || pkt[n] != 0x4A || pkt[n+1] != 0x36 {
		return
	}
	o := n + 2
	id, ok := tryVarint(pkt, &o)
	if !ok || id <= 0 || id > 9_999_999 {
		return
	}
	p.combat.SetSelfID(id)
}

// parseZoneChange maneja 23 36 00 (§8.8): el teletransporte del jugador local a
// la entidad 0, es decir, una carga de zona. Descarta el encuentro actual.
func (p *Parser) parseZoneChange(pkt []byte) {
	_, n := readVarint(pkt, 0)
	if n <= 0 || n+1 >= len(pkt) || pkt[n] != 0x23 || pkt[n+1] != 0x36 {
		return
	}
	o := n + 2
	if id, ok := tryVarint(pkt, &o); ok && id == 0 {
		p.combat.ResetEncounter()
	}
}

// parseGroupExit maneja los frames exactos 13 92 00 00 / 1D 97 00 00 (§8.8), que
// indican que el jugador local ha salido del grupo o de la raid.
func (p *Parser) parseGroupExit(pkt []byte, a, b byte) {
	_, n := readVarint(pkt, 0)
	if n <= 0 || n+4 > len(pkt) {
		return
	}
	if pkt[n] == a && pkt[n+1] == b && pkt[n+2] == 0 && pkt[n+3] == 0 {
		p.combat.ClearParty()
	}
}

// damageSeen indica si una clave de daño ya se procesó.
func (p *Parser) damageSeen(key string) bool {
	_, ok := p.seenDamage[key]
	return ok
}

// markDamageSeen registra una clave de daño en el caché LRU, desalojando la
// entrada más antigua al llegar al límite de slots.
func (p *Parser) markDamageSeen(key string) {
	if _, ok := p.seenDamage[key]; ok {
		return
	}
	if old := p.seenOrder[p.seenNext]; old != "" {
		delete(p.seenDamage, old)
	}
	p.seenOrder[p.seenNext] = key
	p.seenDamage[key] = struct{}{}
	p.seenNext = (p.seenNext + 1) % damageLRUSlots
}

// parseSummonSpawn maneja las apariciones 41 36 (§8.1): marca la entidad como
// NPC y, en el caso de los summons, intenta resolver su dueño.
func (p *Parser) parseSummonSpawn(pkt []byte, ts time.Time) {
	_, n := readVarint(pkt, 0)
	if n <= 0 || n+3 >= len(pkt) {
		return
	}
	if pkt[n] != 0x41 || pkt[n+1] != 0x36 {
		return
	}
	o := n + 2
	id, ok := tryVarint(pkt, &o)
	if !ok || id <= 0 {
		return
	}
	if id > 1_000_000 {
		id = (id & 0x3FFF) | 0x4000
	}
	if o+4 > len(pkt) {
		return
	}
	mask := binary.LittleEndian.Uint32(pkt[o:])
	kind := byte(mask & 0xFF)

	// El nombre, si lo hay, va embebido tras una cabecera; se prueba en las dos
	// posiciones donde el patrón lo coloca habitualmente.
	for _, pos := range []int{o + 4, o + 2} {
		if pos+1 < len(pkt) && pkt[pos]&1 != 0 {
			ln := int(pkt[pos+1])
			if nm, ok := exactName(pkt, pos+2, ln); ok {
				p.combat.SetName(id, nm)
				break
			}
		}
	}

	owner := 0
	switch kind {
	case 0x5F, 0x1F, 0x1D, 0x5D:
		owner = p.summonOwner(pkt, o, id)
	}
	p.combat.MarkNPC(id, owner)
}

// summonOwner resuelve el dueño de un summon usando los patrones conocidos
// (§8.1): busca una firma fija de 8 bytes y lee el varint del id del dueño que
// la sigue.
func (p *Parser) summonOwner(pkt []byte, maskOff, id int) int {
	magic := []byte{0x80, 0x75, 0xD5, 0x2A, 0xBB, 0x03, 0x00, 0x00}
	end := min(maskOff+240, len(pkt))
	for i := maskOff; i+len(magic) < end; i++ {
		if !matchAt(pkt, i, magic) {
			continue
		}
		off := i + len(magic)
		if owner, ok := tryVarint(pkt, &off); ok && owner > 0 && owner != id {
			return owner
		}
	}
	return 0
}

// plausibleID indica si id parece un id de entidad válido: entre 100 y
// 9_999_999, o por debajo de 100 solo si un spawn/identidad ya lo anunció.
func (p *Parser) plausibleID(id int) bool {
	if id >= 100 && id <= 9_999_999 {
		return true
	}
	if id <= 0 {
		return false
	}
	return p.combat.hasID(id)
}

// matchAt comprueba si el patrón pat aparece exactamente en b[at:].
func matchAt(b []byte, at int, pat []byte) bool {
	if at+len(pat) > len(b) {
		return false
	}
	for i, c := range pat {
		if b[at+i] != c {
			return false
		}
	}
	return true
}
