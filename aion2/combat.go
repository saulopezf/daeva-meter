package aion2

import (
	"sort"
	"sync"
	"time"
)

// idleTimeout es cuánto tiempo sigue activo un encuentro tras el último golpe.
// Cuando transcurre, el encuentro se congela; el siguiente golpe válido inicia
// uno nuevo.
const idleTimeout = 5 * time.Second

// Entity es un objeto de juego visto en el cable: un jugador, un NPC o un summon.
type Entity struct {
	ID       int
	Name     string
	Class    int
	Level    int
	Server   int
	IsPlayer bool
	Self     bool
	NPC      bool
	Owner    int // id del dueño para summons/mascotas
}

// Hit es un evento de daño/curación producido por el parser de daño.
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

// MemberStat es el daño agregado por actor tal y como aparece en la tabla de DPS.
type MemberStat struct {
	ID     int     `json:"id"`
	Key    string  `json:"key"`
	Name   string  `json:"name"`
	Class  string  `json:"class"`
	Color  string  `json:"color"`
	Damage int64   `json:"damage"`
	DPS    float64 `json:"dps"`
	Hits   int     `json:"hits"`
	Crits  int     `json:"crits"`
	Self   bool    `json:"self"`
}

// SkillStat es el daño agregado de una habilidad concreta para un miembro.
//
// Hits cuenta eventos de daño (coincide con MemberStat.Hits). ExtraHits son los
// golpes adicionales que el protocolo agrupa dentro de un mismo evento; TotalHits
// es la suma de ambos y es lo que se muestra como "golpes" en la interfaz.
type SkillStat struct {
	ID        int     `json:"id"`
	Name      string  `json:"name"`
	Damage    int64   `json:"damage"`
	DPS       float64 `json:"dps"`
	Share     float64 `json:"share"`
	Hits      int     `json:"hits"`
	ExtraHits int     `json:"extraHits"`
	TotalHits int     `json:"totalHits"`
	Crits     int     `json:"crits"`
	AvgHit    float64 `json:"avgHit"`
}

// MemberDetail es el desglose por habilidad de un miembro del encuentro actual.
type MemberDetail struct {
	Key       string      `json:"key"`
	Name      string      `json:"name"`
	Class     string      `json:"class"`
	Color     string      `json:"color"`
	Self      bool        `json:"self"`
	Found     bool        `json:"found"`
	Active    bool        `json:"active"`
	Elapsed   float64     `json:"elapsed"`
	Damage    int64       `json:"damage"`
	DPS       float64     `json:"dps"`
	Hits      int         `json:"hits"`
	ExtraHits int         `json:"extraHits"`
	TotalHits int         `json:"totalHits"`
	Crits     int         `json:"crits"`
	Skills    []SkillStat `json:"skills"`
}

// Snapshot es el estado de combate listo para serializar a JSON y enviar al
// frontend.
type Snapshot struct {
	Active  bool         `json:"active"`
	Elapsed float64      `json:"elapsed"`
	SelfID  int          `json:"selfId"`
	Self    string       `json:"self"`
	Members []MemberStat `json:"members"`
	Party   []string     `json:"party"`
}

// hitKey identifica un golpe dentro del encuentro. Se guarda también el target
// para poder añadir en el futuro el desglose de daño por enemigo sin tocar el
// parser ni el modelo de eventos.
type hitKey struct {
	actor  int
	skill  int
	target int
}

// hitAcc acumula los datos de uno o varios golpes.
type hitAcc struct {
	damage    int64
	hits      int
	crits     int
	extraHits int
}

// add suma otro acumulador.
func (a *hitAcc) add(b *hitAcc) {
	a.damage += b.damage
	a.hits += b.hits
	a.crits += b.crits
	a.extraHits += b.extraHits
}

// encounter es una pelea: daño acumulado por actor más su ventana temporal.
type encounter struct {
	start time.Time
	last  time.Time

	damage map[int]int64
	hits   map[int]int
	crits  map[int]int

	// breakdown conserva el detalle por actor, habilidad y objetivo. Es la base
	// del desglose por habilidad y, en una segunda iteración, del desglose por
	// enemigo.
	breakdown map[hitKey]*hitAcc
}

func newEncounter() *encounter {
	return &encounter{
		damage:    map[int]int64{},
		hits:      map[int]int{},
		crits:     map[int]int{},
		breakdown: map[hitKey]*hitAcc{},
	}
}

type partyInfo struct {
	Class int
	Level int
}

// Combat acumula encuentros para la tabla de DPS. Solo se rastrea al jugador
// local y a los miembros conocidos del grupo; el daño de otros jugadores se
// descarta.
type Combat struct {
	mu sync.Mutex

	// OnHit, si se define, se llama por cada evento de daño que llega a AddHit.
	// Lo usan los tests.
	OnHit func(Hit)

	entities map[int]*Entity
	byName   map[string]int

	cur *encounter

	selfID int

	partyNames map[string]partyInfo
	party      map[int]struct{}
}

// NewCombat devuelve un modelo de combate vacío.
func NewCombat() *Combat {
	return &Combat{
		entities:   map[int]*Entity{},
		byName:     map[string]int{},
		partyNames: map[string]partyInfo{},
		party:      map[int]struct{}{},
	}
}

// ensure devuelve la entidad con ese id, creándola si aún no existe.
func (c *Combat) ensure(id int) *Entity {
	e := c.entities[id]
	if e == nil {
		e = &Entity{ID: id}
		c.entities[id] = e
	}
	return e
}

// SetSelfID registra el id de entidad del jugador local (de 33 36 o 4A 36).
// Si antes no conocíamos quién éramos, se descarta el encuentro en curso:
// podría contener daño de otros jugadores contado en modo permisivo.
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
		c.cur = nil
	}
}

// SetIdentity registra el nombre/clase/nivel de una entidad. Para self también
// fija servidor, clase y nivel, y descarta el encuentro si hasta ahora no nos
// conocíamos.
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

// SetName registra un nombre descubierto por un registro que no es de identidad
// (spawn, botín...).
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

// MarkNPC marca una entidad como objeto no jugador. owner, si es > 0, es el id
// de su dueño (mascotas/summons).
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

// SetParty reemplaza el roster de grupo conocido. Es autoritativo: vaciar y
// volver a escanear mantiene ids y nombres coherentes.
func (c *Combat) SetParty(members []rosterMember) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.partyNames = map[string]partyInfo{}
	c.party = map[int]struct{}{}
	for _, m := range members {
		c.addPartyLocked(m.Name, m.Class, m.Level)
	}
}

// ClearParty olvida el grupo (al salir de grupo o raid).
func (c *Combat) ClearParty() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.partyNames = map[string]partyInfo{}
	c.party = map[int]struct{}{}
}

// addPartyLocked añade un nombre al roster y, si ya conocemos su id, lo enlaza.
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

// resolvePartyLocked enlaza una entidad con el grupo si su nombre está en el
// roster. Permite que llegue la identidad antes que el roster y viceversa.
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

// hasID indica si el id ya se ha visto como entidad.
func (c *Combat) hasID(id int) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.entities[id]
	return ok
}

// isMob indica si el id corresponde a un NPC.
func (c *Combat) isMob(id int) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.entities[id]
	return e != nil && e.NPC
}

// isKnownPlayer indica si el id corresponde a un jugador conocido.
func (c *Combat) isKnownPlayer(id int) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.entities[id]
	return e != nil && e.IsPlayer
}

// isGroupActorLocked indica si el actor de un daño debe contarse. Antes de
// conocer al jugador local se acepta todo (mejor mostrar algo que nada); después
// es estricto: uno mismo, el grupo conocido, o un summon/mascota cuyo dueño
// pertenece al grupo.
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

// AddHit incorpora un evento de daño al encuentro actual. El daño recibido
// (habilidades de NPC), el auto-daño y el daño de jugadores fuera del grupo se
// ignoran.
//
// Si el encuentro actual lleva más de idleTimeout sin actividad, se abre uno
// nuevo. El total de daño es Damage + ExtraDamage (golpes adicionales del mismo
// lanzamiento).
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

	key := hitKey{actor: h.Actor, skill: h.Skill, target: h.Target}
	acc := c.cur.breakdown[key]
	if acc == nil {
		acc = &hitAcc{}
		c.cur.breakdown[key] = acc
	}
	acc.damage += int64(total)
	acc.hits++
	if h.Crit {
		acc.crits++
	}
	acc.extraHits += h.ExtraHits

	if c.cur.start.IsZero() {
		c.cur.start = ts
	}
	c.cur.last = ts
}

// ResetEncounter descarta el encuentro actual y el congelado (cambio de zona).
func (c *Combat) ResetEncounter() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cur = nil
}

// Snapshot construye la tabla de DPS actual. now es el reloj de pared que decide
// si el encuentro sigue activo. Un encuentro congelado (terminado) se mantiene
// visible hasta que empiece el siguiente.
//
// Agrega por nombre canónico y no por id de entidad: un jugador puede tener
// varios ids (los summons y hechizos de suelo llevan el nombre del dueño, y el
// juego puede reasignar ids al cargar zona). Sumar todo lo que resuelve al mismo
// nombre produce una fila por jugador, con sus mascotas incluidas.
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

	rows, enc, active, elapsed := c.rowsLocked(now)
	if enc == nil {
		return snap
	}
	snap.Active = active
	snap.Elapsed = elapsed
	for _, r := range rows {
		cls, color := ClassName(r.class)
		if cls == "" {
			cls = "?"
		}
		snap.Members = append(snap.Members, MemberStat{
			ID:     r.id,
			Key:    r.key,
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

// MemberDetail devuelve el desglose por habilidad de la fila canónica key. Si el
// encuentro no existe o la fila no está presente, Found es false.
func (c *Combat) MemberDetail(now time.Time, key string) MemberDetail {
	c.mu.Lock()
	defer c.mu.Unlock()

	detail := MemberDetail{
		Key:    key,
		Active: false,
		Skills: make([]SkillStat, 0),
	}
	rows, enc, active, elapsed := c.rowsLocked(now)
	if enc == nil {
		return detail
	}
	detail.Active = active
	detail.Elapsed = elapsed

	r := rows[key]
	if r == nil {
		return detail
	}
	detail.Found = true
	detail.Name = r.name
	detail.Class, detail.Color = ClassName(r.class)
	if detail.Class == "" {
		detail.Class = "?"
	}
	detail.Self = r.self
	detail.Damage = r.damage
	detail.DPS = float64(r.damage) / elapsed
	detail.Hits = r.hits
	detail.Crits = r.crits

	bySkill := map[int]*hitAcc{}
	for hk, acc := range enc.breakdown {
		actorKey, _ := c.actorKeyLocked(c.entities[hk.actor], hk.actor)
		if actorKey != key {
			continue
		}
		dst := bySkill[hk.skill]
		if dst == nil {
			dst = &hitAcc{}
			bySkill[hk.skill] = dst
		}
		dst.add(acc)
	}

	detail.Skills = make([]SkillStat, 0, len(bySkill))
	for id, acc := range bySkill {
		share := 0.0
		if r.damage > 0 {
			share = float64(acc.damage) / float64(r.damage) * 100
		}
		totalHits := acc.hits + acc.extraHits
		avgHit := 0.0
		if totalHits > 0 {
			avgHit = float64(acc.damage) / float64(totalHits)
		}
		detail.ExtraHits += acc.extraHits
		detail.TotalHits += totalHits
		detail.Skills = append(detail.Skills, SkillStat{
			ID:        id,
			Name:      SkillName(id),
			Damage:    acc.damage,
			DPS:       float64(acc.damage) / elapsed,
			Share:     share,
			Hits:      acc.hits,
			ExtraHits: acc.extraHits,
			TotalHits: totalHits,
			Crits:     acc.crits,
			AvgHit:    avgHit,
		})
	}
	sort.Slice(detail.Skills, func(i, j int) bool {
		if detail.Skills[i].Damage != detail.Skills[j].Damage {
			return detail.Skills[i].Damage > detail.Skills[j].Damage
		}
		if detail.Skills[i].Name != detail.Skills[j].Name {
			return detail.Skills[i].Name < detail.Skills[j].Name
		}
		return detail.Skills[i].ID < detail.Skills[j].ID
	})
	return detail
}

// rowsLocked agrega el encuentro actual por nombre canónico y devuelve también
// la ventana temporal usada para DPS. Requiere c.mu.
func (c *Combat) rowsLocked(now time.Time) (map[string]*aggRow, *encounter, bool, float64) {
	enc := c.cur
	if enc == nil {
		return nil, nil, false, 0
	}
	active := now.Sub(enc.last) < idleTimeout
	elapsed := enc.last.Sub(enc.start).Seconds()
	if elapsed < 0.001 {
		elapsed = 0.001
	}

	rows := map[string]*aggRow{}
	for id, dmg := range enc.damage {
		e := c.entities[id]
		key, name := c.actorKeyLocked(e, id)
		r := rows[key]
		if r == nil {
			r = &aggRow{key: key, id: id, name: name}
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
		// Se prefiere el id y la clase de una entidad jugador real antes que los
		// de uno de sus summons.
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
	return rows, enc, active, elapsed
}

// aggRow acumula el daño de todas las entidades que resuelven al mismo nombre
// canónico de jugador.
type aggRow struct {
	key    string
	id     int
	name   string
	class  int
	self   bool
	player bool // se activa al fusionar una entidad jugador real (no summon)
	damage int64
	hits   int
	crits  int
}

// actorKeyLocked asigna un actor a su fila canónica en la tabla. Los summons y
// hechizos de suelo (entidades NPC con el nombre de su dueño) resuelven al
// nombre del dueño; las entidades sin nombre caen a su id.
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

// Entities devuelve una instantánea de todas las entidades vistas (para depurar
// o reproducir capturas).
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

// itoa convierte un int a decimal sin asignaciones de memoria.
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
