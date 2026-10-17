package aion2

import (
	"encoding/binary"
	"sync"
	"time"
)

const (
	maxStreamBuf   = 2 << 20 // 2 MiB per stream before resetting
	damageLRUSlots = 16384
)

// Parser turns a server->client byte stream into combat events. It keeps one
// reassembly buffer per TCP stream and feeds a shared Combat model.
type Parser struct {
	combat *Combat

	mu   sync.Mutex
	bufs map[int64][]byte
	now  time.Time

	seenDamage map[string]struct{}
	seenOrder  []string
	seenNext   int
}

// NewParser returns a parser writing into combat.
func NewParser(combat *Combat) *Parser {
	return &Parser{
		combat:     combat,
		bufs:       map[int64][]byte{},
		seenDamage: map[string]struct{}{},
		seenOrder:  make([]string, damageLRUSlots),
	}
}

// Feed appends freshly reassembled bytes for one stream and parses as many
// complete frames as possible. ts is the capture time of the data.
func (p *Parser) Feed(streamID int64, data []byte, ts time.Time) {
	if len(data) == 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	buf := append(p.bufs[streamID], data...)
	if len(buf) > maxStreamBuf {
		buf = buf[:0]
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

// ConsumeStream parses one buffer in place (used by tests and replay).
func (p *Parser) ConsumeStream(buf []byte, ts time.Time) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.consume(buf, ts)
}

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

// scanRegion runs the pattern scanners (identity, roster) over byte regions,
// and also opens bundle payloads embedded inside packets (spec §6.2), where
// copies of the identity and the party roster travel.
func (p *Parser) scanRegion(b []byte, regs [][2]int) {
	p.scanIdentity(b, regs)
	p.scanRoster(b, regs)
	p.scanEmbeddedBundles(b, regs, 1)
}

// scanEmbeddedBundles finds FF FF bundle payloads inside packets whose leading
// length varint ends exactly where the bundle begins, decompresses them and
// applies only the identity and roster scanners.
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

// tryEmbeddedBundle attempts to decode one embedded bundle starting at i (the
// FF FF magic). It returns true when a bundle was successfully processed.
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

// parsePacket runs every parser over one frame; each ignores what is not its
// own opcode.
func (p *Parser) parsePacket(pkt []byte, ts time.Time) {
	_, n := readVarint(pkt, 0)
	if n <= 0 || n+1 >= len(pkt) {
		return
	}
	op := [2]byte{pkt[n], pkt[n+1]}
	switch op {
	case [2]byte{0x04, 0x38}:
		p.parseDamage(pkt, enableEmbeddedDamage, false)
	case opIdentitySelf:
		p.parseIdentityAt(pkt, n, true)
	case opIdentityOther:
		p.parseIdentityAt(pkt, n, false)
	case [2]byte{0x41, 0x36}: // summon / mob spawn
		p.parseSummonSpawn(pkt, ts)
	case [2]byte{0x4A, 0x36}: // local player stats: identifies self
		p.parseSelfStats(pkt)
	case [2]byte{0x23, 0x36}: // teleport of entity 0 = zone change
		p.parseZoneChange(pkt)
	case [2]byte{0x13, 0x92}: // left the party
		p.parseGroupExit(pkt, 0x13, 0x92)
	case [2]byte{0x1D, 0x97}: // left the raid
		p.parseGroupExit(pkt, 0x1D, 0x97)
	}
}

// parseSelfStats handles 4A 36 <id> (§8.8): the local player's stats, which
// identify the entity id of our own character even without a 33 36 identity.
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

// parseZoneChange handles 23 36 00 (§8.8): teleport of the local player to
// entity 0, i.e. a zone load. The current encounter is discarded.
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

// parseGroupExit handles the exact frames 13 92 00 00 / 1D 97 00 00 (§8.8).
func (p *Parser) parseGroupExit(pkt []byte, a, b byte) {
	_, n := readVarint(pkt, 0)
	if n <= 0 || n+4 > len(pkt) {
		return
	}
	if pkt[n] == a && pkt[n+1] == b && pkt[n+2] == 0 && pkt[n+3] == 0 {
		p.combat.ClearParty()
	}
}

func (p *Parser) damageSeen(key string) bool {
	_, ok := p.seenDamage[key]
	return ok
}

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

// parseSummonSpawn handles 41 36 spawns (§8.1): it flags the entity as an NPC
// and, for summons, tries to resolve its owner.
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

// summonOwner resolves the owner of a summon using the known patterns (§8.1).
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

// plausibleID reports whether id looks like an entity id: 100..9_999_999, or
// below 100 only if a spawn/identity already announced it.
func (p *Parser) plausibleID(id int) bool {
	if id >= 100 && id <= 9_999_999 {
		return true
	}
	if id <= 0 {
		return false
	}
	return p.combat.hasID(id)
}

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
