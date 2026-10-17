package aion2

import (
	"sort"
	"sync"
	"time"
)

// idleTimeout is how long an encounter stays active after the last hit. When it
// elapses the encounter is frozen; the next qualifying hit starts a new one.
const idleTimeout = 5 * time.Second

// Entity is a game object seen on the wire: a player, an NPC or a summon.
type Entity struct {
	ID       int
	Name     string
	Class    int
	Level    int
	Server   int
	IsPlayer bool
	Self     bool
	NPC      bool
	Owner    int // owner id for summons/pets
}

// Hit is one damage/heal event produced by the damage parser.
type Hit struct {
	Target      int
	Actor       int
	Skill       int
	HitType     int
	Crit        bool
	Damage      int
	ExtraHits   int
	ExtraDamage int
	Heal        int
	PowerScalar int
	NPCSkill    bool
}

// MemberStat is a per-actor damage aggregate as shown on the DPS board.
type MemberStat struct {
	ID     int     `json:"id"`
	Name   string  `json:"name"`
	Class  string  `json:"class"`
	Color  string  `json:"color"`
	Damage int64   `json:"damage"`
	DPS    float64 `json:"dps"`
	Hits   int     `json:"hits"`
	Crits  int     `json:"crits"`
	Self   bool    `json:"self"`
}

// Snapshot is the JSON-friendly combat state pushed to the frontend.
type Snapshot struct {
	Active  bool         `json:"active"`
	Elapsed float64      `json:"elapsed"`
	SelfID  int          `json:"selfId"`
	Self    string       `json:"self"`
	Members []MemberStat `json:"members"`
	Party   []string     `json:"party"`
}

// encounter is one fight: per-actor damage totals plus its time window.
type encounter struct {
	start  time.Time
	last   time.Time
	damage map[int]int64
	hits   map[int]int
	crits  map[int]int
}

func newEncounter() *encounter {
	return &encounter{
		damage: map[int]int64{},
		hits:   map[int]int{},
		crits:  map[int]int{},
	}
}

type partyInfo struct {
	Class int
	Level int
}

// Combat accumulates encounters for the DPS board. Only the local player and
// known group members are tracked; damage from other players is discarded.
type Combat struct {
	mu sync.Mutex

	// OnHit, when set, is called for every damage event that reaches AddHit.
	// Used by tests.
	OnHit func(Hit)

	entities map[int]*Entity
	byName   map[string]int

	cur *encounter

	selfID int

	partyNames map[string]partyInfo
	party      map[int]struct{}
}

// NewCombat returns an empty combat model.
func NewCombat() *Combat {
	return &Combat{
		entities:   map[int]*Entity{},
		byName:     map[string]int{},
		partyNames: map[string]partyInfo{},
		party:      map[int]struct{}{},
	}
}

func (c *Combat) ensure(id int) *Entity {
	e := c.entities[id]
	if e == nil {
		e = &Entity{ID: id}
		c.entities[id] = e
	}
	return e
}

// SetSelfID records the local player's entity id (from 33 36 or 4A 36).
func (c *Combat) SetSelfID(id int) {
	if id <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	wasUnknown := c.selfID == 0
	c.selfID = id
	e := c.ensure(id)
	e.Self = true
	e.IsPlayer = true
	if wasUnknown {
		// Drop anything counted while we still did not know who we were.
		c.cur = nil
	}
}

// SetIdentity records the name/class/level of an entity.
func (c *Combat) SetIdentity(id int, name string, self bool, server, class, level int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.ensure(id)
	if name != "" {
		e.Name = name
		c.byName[name] = id
		c.resolvePartyLocked(id, name)
	}
	if self {
		wasUnknown := c.selfID == 0
		e.Self = true
		e.IsPlayer = true
		e.Server, e.Class, e.Level = server, class, level
		c.selfID = id
		if wasUnknown {
			c.cur = nil
		}
	} else if name != "" {
		e.IsPlayer = true
	}
}

// SetName records a name discovered by a non-identity record (spawn, loot...).
func (c *Combat) SetName(id int, name string) {
	if name == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.ensure(id)
	e.Name = name
	c.byName[name] = id
	c.resolvePartyLocked(id, name)
}

// MarkNPC flags an entity as a non-player object.
func (c *Combat) MarkNPC(id int, owner int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if id <= 0 {
		return
	}
	e := c.ensure(id)
	e.NPC = true
	if owner > 0 {
		e.Owner = owner
	}
}

// SetParty replaces the known group roster. It is authoritative: clearing and
// rescanning keeps ids and names consistent.
func (c *Combat) SetParty(members []rosterMember) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.partyNames = map[string]partyInfo{}
	c.party = map[int]struct{}{}
	for _, m := range members {
		c.addPartyLocked(m.Name, m.Class, m.Level)
	}
}

// ClearParty forgets the group (on group/raid exit).
func (c *Combat) ClearParty() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.partyNames = map[string]partyInfo{}
	c.party = map[int]struct{}{}
}

func (c *Combat) addPartyLocked(name string, class, level int) {
	if name == "" {
		return
	}
	c.partyNames[name] = partyInfo{Class: class, Level: level}
	if id, ok := c.byName[name]; ok {
		c.party[id] = struct{}{}
		e := c.ensure(id)
		if e.Class == 0 {
			e.Class = class
		}
		if e.Level == 0 {
			e.Level = level
		}
	}
}

// resolvePartyLocked links an entity to the group if its name is on the roster.
func (c *Combat) resolvePartyLocked(id int, name string) {
	info, ok := c.partyNames[name]
	if !ok {
		return
	}
	c.party[id] = struct{}{}
	e := c.ensure(id)
	if e.Class == 0 {
		e.Class = info.Class
	}
	if e.Level == 0 {
		e.Level = info.Level
	}
}

func (c *Combat) hasID(id int) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.entities[id]
	return ok
}

func (c *Combat) isMob(id int) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.entities[id]
	return e != nil && e.NPC
}

func (c *Combat) isKnownPlayer(id int) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.entities[id]
	return e != nil && e.IsPlayer
}

// isGroupActorLocked reports whether a damage actor should be tracked. Before
// the local player is known it accepts everything (better than showing
// nothing); afterwards it is strict: self, known group, or a summon/pet whose
// owner belongs to the group.
func (c *Combat) isGroupActorLocked(id int) bool {
	if c.selfID == 0 {
		return true
	}
	if id == c.selfID {
		return true
	}
	if _, ok := c.party[id]; ok {
		return true
	}
	if e := c.entities[id]; e != nil && e.Owner > 0 {
		if e.Owner == c.selfID {
			return true
		}
		_, ok := c.party[e.Owner]
		return ok
	}
	return false
}

// AddHit folds a damage event into the current encounter. Damage received (NPC
// skills), self-damage and damage from non-group players are ignored.
func (c *Combat) AddHit(ts time.Time, h Hit) {
	if h.NPCSkill {
		return
	}
	total := h.Damage + h.ExtraDamage
	if total <= 0 || h.Actor <= 0 || h.Actor == h.Target {
		return
	}
	if c.OnHit != nil {
		c.OnHit(h)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.isGroupActorLocked(h.Actor) {
		return
	}
	if c.cur == nil || ts.Sub(c.cur.last) > idleTimeout {
		c.cur = newEncounter()
	}
	c.cur.damage[h.Actor] += int64(total)
	c.cur.hits[h.Actor]++
	if h.Crit {
		c.cur.crits[h.Actor]++
	}
	if c.cur.start.IsZero() {
		c.cur.start = ts
	}
	c.cur.last = ts
}

// ResetEncounter discards the current and frozen encounter (zone change).
func (c *Combat) ResetEncounter() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cur = nil
}

// Snapshot builds the current DPS board. now is the wall clock used to decide
// whether the encounter is still active. A frozen (finished) encounter is kept
// visible until the next one starts.
func (c *Combat) Snapshot(now time.Time) Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()

	snap := Snapshot{SelfID: c.selfID}
	if self := c.entities[c.selfID]; self != nil {
		snap.Self = self.Name
	}
	for name := range c.partyNames {
		snap.Party = append(snap.Party, name)
	}
	sort.Strings(snap.Party)

	enc := c.cur
	if enc == nil {
		return snap
	}
	snap.Active = now.Sub(enc.last) < idleTimeout
	snap.Elapsed = enc.last.Sub(enc.start).Seconds()
	if snap.Elapsed < 0.001 {
		snap.Elapsed = 0.001
	}
	// Aggregate by canonical name, not by entity id: a player may own several
	// ids (summons and ground spells carry the owner's inline name, and the
	// game may reassign ids on a zone load). Summing everything that resolves to
	// the same name yields one row per player with their pets included.
	rows := map[string]*aggRow{}
	for id, dmg := range enc.damage {
		e := c.entities[id]
		key, name := c.actorKeyLocked(e, id)
		r := rows[key]
		if r == nil {
			r = &aggRow{id: id, name: name}
			rows[key] = r
		}
		r.damage += dmg
		r.hits += enc.hits[id]
		r.crits += enc.crits[id]
		if e == nil {
			continue
		}
		if e.Self {
			r.self = true
		}
		// Prefer the row's id and class from a real player entity rather than
		// from one of their summons.
		if realPlayer := e.IsPlayer && !e.NPC; realPlayer && !r.player {
			r.player = true
			r.id = id
			if e.Class != 0 {
				r.class = e.Class
			}
		} else if r.class == 0 && e.Class != 0 {
			r.class = e.Class
		}
	}
	for _, r := range rows {
		cls, color := ClassName(r.class)
		if cls == "" {
			cls = "?"
		}
		snap.Members = append(snap.Members, MemberStat{
			ID:     r.id,
			Name:   r.name,
			Class:  cls,
			Color:  color,
			Damage: r.damage,
			DPS:    float64(r.damage) / snap.Elapsed,
			Hits:   r.hits,
			Crits:  r.crits,
			Self:   r.self,
		})
	}
	sort.Slice(snap.Members, func(i, j int) bool {
		if snap.Members[i].Damage != snap.Members[j].Damage {
			return snap.Members[i].Damage > snap.Members[j].Damage
		}
		return snap.Members[i].Name < snap.Members[j].Name
	})
	return snap
}

// aggRow accumulates the damage of every entity id that resolves to the same
// canonical player name.
type aggRow struct {
	id     int
	name   string
	class  int
	self   bool
	player bool // set once a real (non-summon) player entity was merged in
	damage int64
	hits   int
	crits  int
}

// actorKeyLocked maps an actor to its canonical board row. Summons and ground
// spells (NPC entities named after their owner) resolve to the owner's name;
// entities without a name fall back to their id.
func (c *Combat) actorKeyLocked(e *Entity, id int) (key, name string) {
	if e != nil {
		if e.Name != "" {
			return "n:" + e.Name, e.Name
		}
		if e.Owner > 0 {
			if o := c.entities[e.Owner]; o != nil && o.Name != "" {
				return "n:" + o.Name, o.Name
			}
		}
	}
	return "i:" + itoa(id), "#" + itoa(id)
}

// Entities returns a snapshot of every entity seen (for debugging/replay).
func (c *Combat) Entities() []Entity {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Entity, 0, len(c.entities))
	for _, e := range c.entities {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// itoa converts a non-negative int to decimal without allocations.
func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
