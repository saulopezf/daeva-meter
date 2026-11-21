package aion2

import (
	"sort"
	"sync"
	"time"
)

const (
	// idleTimeout es cuánto tiempo sigue activo un encuentro tras el último
	// golpe. Cuando transcurre, el encuentro se congela; el siguiente golpe
	// válido inicia uno nuevo.
	idleTimeout = 5 * time.Second

	// phaseGrace permite continuar un encuentro cuando el objetivo ya había
	// recibido daño, aunque el grupo haya dejado de atacarlo durante una
	// transición de fase. Solo se aplica a objetivos ya vistos: un enemigo
	// distinto después del timeout sigue abriendo un encuentro nuevo.
	phaseGrace = 30 * time.Second

	// bossGrace es la ventana de un encuentro contra un jefe. Es más larga
	// porque las fases pueden dejar al jefe sin recibir daño durante varios
	// segundos. El encuentro se cierra antes si el jefe desaparece (42 36).
	bossGrace = 60 * time.Second
)

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
	Boss     bool
	Removed  bool
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

// Metric identifica una de las tres vistas de la sesión.
type Metric string

const (
	MetricDamage  Metric = "damage"
	MetricHealing Metric = "healing"
	MetricTaken   Metric = "taken"
)

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

// MetricStat es una fila de las tablas de curación y daño recibido.
type MetricStat struct {
	ID        int     `json:"id"`
	Key       string  `json:"key"`
	Name      string  `json:"name"`
	Class     string  `json:"class"`
	Color     string  `json:"color"`
	Value     int64   `json:"value"`
	PerSecond float64 `json:"perSecond"`
	Hits      int     `json:"hits"`
	Crits     int     `json:"crits"`
	Self      bool    `json:"self"`
}

// EncounterSummary es una entrada del historial de combates de la sesión.
type EncounterSummary struct {
	ID           int     `json:"id"`
	Title        string  `json:"title"`
	Boss         string  `json:"boss"`
	BossID       int     `json:"bossId"`
	IsBoss       bool    `json:"isBoss"`
	Active       bool    `json:"active"`
	StartedAt    int64   `json:"startedAt"`
	Elapsed      float64 `json:"elapsed"`
	TotalDamage  int64   `json:"totalDamage"`
	TotalHealing int64   `json:"totalHealing"`
	TotalTaken   int64   `json:"totalTaken"`
}

// SkillStat es el daño agregado de una habilidad concreta para un miembro.
//
// Hits cuenta eventos de daño (coincide con MemberStat.Hits). ExtraHits son los
// golpes adicionales que el protocolo agrupa dentro de un mismo evento; TotalHits
// es la suma de ambos y es lo que se muestra como "golpes" en la interfaz.
type SkillStat struct {
	ID        int     `json:"id"`
	Key       string  `json:"key"`
	Name      string  `json:"name"`
	Source    string  `json:"source"`
	Icon      string  `json:"icon"`
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
// Damage y DPS contienen el valor de la métrica seleccionada en Metric, de modo
// que la misma ventana sirve para daño, curación y daño recibido.
type MemberDetail struct {
	Key       string      `json:"key"`
	Name      string      `json:"name"`
	Class     string      `json:"class"`
	Color     string      `json:"color"`
	Self      bool        `json:"self"`
	Found     bool        `json:"found"`
	Active    bool        `json:"active"`
	Metric    Metric      `json:"metric"`
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
// frontend. Members es la tabla de daño infligido; Healing y Taken son las
// otras dos pestañas.
type Snapshot struct {
	ID        int          `json:"id"`
	Active    bool         `json:"active"`
	StartedAt int64        `json:"startedAt"`
	Elapsed   float64      `json:"elapsed"`
	Boss      string       `json:"boss"`
	BossID    int          `json:"bossId"`
	IsBoss    bool         `json:"isBoss"`
	SelfID    int          `json:"selfId"`
	Self      string       `json:"self"`
	Members   []MemberStat `json:"members"`
	Healing   []MetricStat `json:"healing"`
	Taken     []MetricStat `json:"taken"`
	Party     []string     `json:"party"`
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

// encounter es una pelea: daño, curación y daño recibido acumulados por actor
// más su ventana temporal.
type encounter struct {
	id    int
	start time.Time
	last  time.Time
	// lastDamage marca el último daño infligido. Es el reloj que decide cuándo
	// se parte en un combate nuevo; así una curación no fusiona dos peleas.
	lastDamage time.Time

	damage map[int]int64
	hits   map[int]int
	crits  map[int]int

	healing   map[int]int64
	healHits  map[int]int
	healCrits map[int]int

	taken      map[int]int64
	takenHits  map[int]int
	takenCrits map[int]int

	// breakdown conserva el detalle por actor, habilidad y objetivo. Es la base
	// del desglose por habilidad y, en una segunda iteración, del desglose por
	// enemigo.
	breakdown      map[hitKey]*hitAcc
	healBreakdown  map[hitKey]*hitAcc
	takenBreakdown map[hitKey]*hitAcc

	// targets guarda el último golpe recibido por cada objetivo. Sirve para
	// distinguir una fase del mismo enemigo de un pull contra otro enemigo.
	targets map[int]int64

	// mainTarget es el primer jefe dañado en el encuentro; boss indica que el
	// encuentro es contra un jefe y closed que el jefe ya desapareció.
	mainTarget int
	boss       bool
	closed     bool

	// entities congela las identidades referenciadas al archivar el encuentro.
	// El juego reasigna ids al cargar zona, así que un combate archivado no
	// puede resolver nombres contra el mapa vivo. Es nil mientras está en curso.
	entities map[int]*Entity
}

func newEncounter(id int) *encounter {
	return &encounter{
		id:             id,
		damage:         map[int]int64{},
		hits:           map[int]int{},
		crits:          map[int]int{},
		healing:        map[int]int64{},
		healHits:       map[int]int{},
		healCrits:      map[int]int{},
		taken:          map[int]int64{},
		takenHits:      map[int]int{},
		takenCrits:     map[int]int{},
		breakdown:      map[hitKey]*hitAcc{},
		healBreakdown:  map[hitKey]*hitAcc{},
		takenBreakdown: map[hitKey]*hitAcc{},
		targets:        map[int]int64{},
	}
}

// hasData indica si el encuentro tiene algo que merezca conservarse.
func (e *encounter) hasData() bool {
	return len(e.damage) > 0 || len(e.healing) > 0 || len(e.taken) > 0
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

	cur     *encounter
	history []*encounter
	nextID  int

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
	e.Removed = false
	if owner > 0 {
		e.Owner = owner
	}
}

// MarkBoss marca una entidad como jefe. Los encuentros contra jefes usan una
// ventana de gracia más larga para no dividirse durante las transiciones de fase.
func (c *Combat) MarkBoss(id int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if id <= 0 {
		return
	}
	e := c.ensure(id)
	e.NPC = true
	e.Boss = true
	e.Removed = false
}

// MarkRemoved marca una entidad como retirada (muerte, despawn o salida de
// visibilidad). Si era el jefe principal del encuentro actual, lo cierra para
// que el siguiente golpe abra uno nuevo.
func (c *Combat) MarkRemoved(id int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if id <= 0 {
		return
	}
	e := c.ensure(id)
	e.Removed = true
	if c.cur != nil && c.cur.boss && c.cur.mainTarget == id {
		c.cur.closed = true
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

// isGroupTargetLocked indica si un id es el jugador local, un miembro del grupo
// o una mascota/summon cuyo dueño pertenece al grupo. Se usa para atribuir
// curación y daño recibido sin incluir a jugadores ajenos.
func (c *Combat) isGroupTargetLocked(id int) bool {
	if id <= 0 {
		return false
	}
	if c.selfID > 0 && id == c.selfID {
		return true
	}
	if _, ok := c.party[id]; ok {
		return true
	}
	if e := c.entities[id]; e != nil && e.Owner > 0 {
		if c.selfID > 0 && e.Owner == c.selfID {
			return true
		}
		_, ok := c.party[e.Owner]
		return ok
	}
	return false
}

// AddHit incorpora un evento de combate al encuentro actual: daño infligido,
// curación realizada o daño recibido según el tipo de golpe. El auto-daño y el
// daño de jugadores fuera del grupo se ignoran.
//
// El daño infligido es el que abre y parte encuentros. Curación y daño recibido
// se atribuyen al encuentro activo para no fusionar peleas distintas con una
// curación fuera de combate. El total de daño es Damage + ExtraDamage (golpes
// adicionales del mismo lanzamiento).
func (c *Combat) AddHit(ts time.Time, h Hit) {
	total := h.Damage + h.ExtraDamage
	if h.Actor <= 0 {
		return
	}
	if h.Actor == h.Target {
		if h.NPCSkill {
			return
		}
		total = 0 // el auto-daño nunca cuenta; la autocuración sí
	}
	if total <= 0 && h.Heal <= 0 {
		return
	}
	if c.OnHit != nil {
		c.OnHit(h)
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if h.NPCSkill {
		c.addTakenLocked(ts, h, total)
		return
	}
	if !c.isGroupActorLocked(h.Actor) {
		return
	}

	var enc *encounter
	if total > 0 {
		enc = c.cur
		if enc == nil || enc.closed || c.splitLocked(ts, h.Target) {
			c.archiveCurrentLocked()
			enc = c.newEncounterLocked(ts)
			c.cur = enc
		}
		c.addDamageLocked(enc, ts, h, total)
	} else {
		enc = c.liveEncounterLocked(ts)
	}
	if h.Heal > 0 && enc != nil {
		c.addHealingLocked(enc, ts, h)
	}
}

// addDamageLocked acumula un golpe de daño infligido. Requiere c.mu.
func (c *Combat) addDamageLocked(enc *encounter, ts time.Time, h Hit, total int) {
	if e := c.entities[h.Target]; e != nil && e.Boss {
		enc.boss = true
		if enc.mainTarget == 0 {
			enc.mainTarget = h.Target
		}
	}
	enc.targets[h.Target] = ts.UnixNano()

	enc.damage[h.Actor] += int64(total)
	enc.hits[h.Actor]++
	if h.Crit {
		enc.crits[h.Actor]++
	}

	key := hitKey{actor: h.Actor, skill: h.Skill, target: h.Target}
	acc := enc.breakdown[key]
	if acc == nil {
		acc = &hitAcc{}
		enc.breakdown[key] = acc
	}
	acc.damage += int64(total)
	acc.hits++
	if h.Crit {
		acc.crits++
	}
	acc.extraHits += h.ExtraHits

	if enc.start.IsZero() {
		enc.start = ts
	}
	enc.last = ts
	enc.lastDamage = ts
}

// addHealingLocked acumula una curación realizada por un actor del grupo.
// Requiere c.mu.
func (c *Combat) addHealingLocked(enc *encounter, ts time.Time, h Hit) {
	if h.Heal <= 0 || !c.isGroupTargetLocked(h.Target) {
		return
	}
	enc.healing[h.Actor] += int64(h.Heal)
	enc.healHits[h.Actor]++
	if h.Crit {
		enc.healCrits[h.Actor]++
	}

	key := hitKey{actor: h.Actor, skill: h.Skill, target: h.Target}
	acc := enc.healBreakdown[key]
	if acc == nil {
		acc = &hitAcc{}
		enc.healBreakdown[key] = acc
	}
	acc.damage += int64(h.Heal)
	acc.hits++
	if h.Crit {
		acc.crits++
	}
	enc.last = ts
}

// addTakenLocked acumula un golpe de daño recibido por un miembro del grupo.
// Requiere c.mu.
func (c *Combat) addTakenLocked(ts time.Time, h Hit, total int) {
	if total <= 0 || !c.isGroupTargetLocked(h.Target) {
		return
	}
	enc := c.liveEncounterLocked(ts)
	if enc == nil {
		return
	}
	enc.taken[h.Target] += int64(total)
	enc.takenHits[h.Target]++
	if h.Crit {
		enc.takenCrits[h.Target]++
	}

	key := hitKey{actor: h.Actor, skill: h.Skill, target: h.Target}
	acc := enc.takenBreakdown[key]
	if acc == nil {
		acc = &hitAcc{}
		enc.takenBreakdown[key] = acc
	}
	acc.damage += int64(total)
	acc.hits++
	if h.Crit {
		acc.crits++
	}
	enc.last = ts
}

// liveEncounterLocked devuelve el encuentro actual si todavía admite eventos
// auxiliares (curación o daño recibido). Requiere c.mu.
func (c *Combat) liveEncounterLocked(ts time.Time) *encounter {
	enc := c.cur
	if enc == nil || enc.closed {
		return nil
	}
	gap := ts.Sub(enc.last)
	if enc.boss {
		if gap > bossGrace {
			return nil
		}
	} else if gap > idleTimeout {
		return nil
	}
	return enc
}

// newEncounterLocked crea un encuentro con id secuencial. Requiere c.mu.
func (c *Combat) newEncounterLocked(start time.Time) *encounter {
	c.nextID++
	enc := newEncounter(c.nextID)
	enc.start = start
	return enc
}

// archiveCurrentLocked mueve el encuentro actual al historial. Requiere c.mu.
func (c *Combat) archiveCurrentLocked() {
	if c.cur == nil {
		return
	}
	if c.cur.hasData() {
		c.freezeEntitiesLocked(c.cur)
		c.history = append(c.history, c.cur)
	}
	c.cur = nil
}

// freezeEntitiesLocked copia por valor cada entidad que el encuentro referencia
// (actores, objetivos, jefe y, recursivamente, sus dueños). Requiere c.mu.
func (c *Combat) freezeEntitiesLocked(enc *encounter) {
	frozen := map[int]*Entity{}
	var add func(id int)
	add = func(id int) {
		if id <= 0 {
			return
		}
		if _, ok := frozen[id]; ok {
			return
		}
		e := c.entities[id]
		if e == nil {
			return
		}
		cp := *e
		frozen[id] = &cp
		add(cp.Owner)
	}
	for _, m := range []map[int]int64{enc.damage, enc.healing, enc.taken} {
		for id := range m {
			add(id)
		}
	}
	for _, b := range []map[hitKey]*hitAcc{enc.breakdown, enc.healBreakdown, enc.takenBreakdown} {
		for hk := range b {
			add(hk.actor)
			add(hk.target)
		}
	}
	add(enc.mainTarget)
	enc.entities = frozen
}

// entsFor devuelve el mapa de entidades con el que resolver un encuentro: el
// congelado si está archivado o el vivo si está en curso. Requiere c.mu.
func (c *Combat) entsFor(enc *encounter) map[int]*Entity {
	if enc != nil && enc.entities != nil {
		return enc.entities
	}
	return c.entities
}

// splitLocked decide si un golpe debe abrir un encuentro nuevo. Requiere c.mu.
//
// Un encuentro de jefe se mantiene durante bossGrace para cubrir fases en las
// que el grupo no puede atacar; un jefe distinto siempre abre uno nuevo. En
// encuentros normales, solo se toleran huecos de phaseGrace si el objetivo ya
// había recibido daño en el mismo encuentro, para no fusionar pulls distintos.
// El hueco se mide desde el último daño infligido, no desde la última curación.
func (c *Combat) splitLocked(ts time.Time, target int) bool {
	enc := c.cur
	if enc == nil {
		return true
	}
	// Un jefe distinto siempre abre un encuentro nuevo, aunque llegue dentro
	// del timeout normal (por ejemplo, un add con flag de jefe).
	if enc.boss {
		if e := c.entities[target]; e != nil && e.Boss && target != enc.mainTarget {
			return true
		}
	}
	gap := ts.Sub(enc.lastDamage)
	if gap <= idleTimeout {
		return false
	}
	if enc.boss {
		return gap > bossGrace
	}
	if gap <= phaseGrace {
		_, seen := enc.targets[target]
		return !seen
	}
	return true
}

// ResetEncounter archiva el encuentro actual y lo descarta (cambio de zona).
func (c *Combat) ResetEncounter() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.archiveCurrentLocked()
}

// CloseEncounter marca el encuentro actual como terminado sin archivarlo. El
// siguiente golpe abrirá uno nuevo y el anterior pasará al historial. Se usa al
// detener la captura para que un reinicio no fusione peleas.
func (c *Combat) CloseEncounter() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cur != nil {
		c.cur.closed = true
	}
}

// Snapshot construye las tres tablas del encuentro actual. now es el reloj de
// pared que decide si el encuentro sigue activo. Un encuentro congelado
// (terminado) se mantiene visible hasta que empiece el siguiente.
//
// Agrega por nombre canónico y no por id de entidad: un jugador puede tener
// varios ids (los summons y hechizos de suelo llevan el nombre del dueño, y el
// juego puede reasignar ids al cargar zona). Sumar todo lo que resuelve al mismo
// nombre produce una fila por jugador, con sus mascotas incluidas.
func (c *Combat) Snapshot(now time.Time) Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.snapshotLocked(now, c.cur)
}

// SnapshotFor devuelve un encuentro del historial por id. id <= 0 devuelve el
// encuentro actual.
func (c *Combat) SnapshotFor(now time.Time, id int) Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.snapshotLocked(now, c.encounterLocked(id))
}

// History devuelve los combates anteriores, del más reciente al más antiguo.
func (c *Combat) History(now time.Time) []EncounterSummary {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]EncounterSummary, 0, len(c.history))
	for i := len(c.history) - 1; i >= 0; i-- {
		out = append(out, c.summaryLocked(now, c.history[i]))
	}
	return out
}

// encounterLocked busca un encuentro por id. id <= 0 es el actual.
func (c *Combat) encounterLocked(id int) *encounter {
	if id <= 0 {
		return c.cur
	}
	if c.cur != nil && c.cur.id == id {
		return c.cur
	}
	for _, enc := range c.history {
		if enc.id == id {
			return enc
		}
	}
	return nil
}

// snapshotLocked construye el snapshot de un encuentro concreto. Requiere c.mu.
func (c *Combat) snapshotLocked(now time.Time, enc *encounter) Snapshot {
	snap := Snapshot{SelfID: c.selfID}
	if self := c.entities[c.selfID]; self != nil {
		snap.Self = self.Name
	}
	for name := range c.partyNames {
		snap.Party = append(snap.Party, name)
	}
	sort.Strings(snap.Party)
	if enc == nil {
		return snap
	}

	snap.ID = enc.id
	if !enc.start.IsZero() {
		snap.StartedAt = enc.start.UnixMilli()
	}
	snap.Active, snap.Elapsed = c.windowLocked(now, enc, enc == c.cur)
	if enc.boss && enc.mainTarget > 0 {
		snap.IsBoss = true
		snap.BossID = enc.mainTarget
		name := ""
		if e := c.entsFor(enc)[enc.mainTarget]; e != nil {
			name = e.Name
		}
		if name != "" {
			snap.Boss = name
		} else {
			snap.Boss = "Jefe #" + itoa(enc.mainTarget)
		}
	}

	rows := c.rowsFromLocked(c.entsFor(enc), enc.damage, enc.hits, enc.crits, false)
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
	snap.Healing = c.metricStatsLocked(c.entsFor(enc), enc.healing, enc.healHits, enc.healCrits, false, snap.Elapsed)
	snap.Taken = c.metricStatsLocked(c.entsFor(enc), enc.taken, enc.takenHits, enc.takenCrits, true, snap.Elapsed)
	return snap
}

// windowLocked calcula si un encuentro sigue activo y su duración. current
// distingue el encuentro en curso de uno archivado. Requiere c.mu.
func (c *Combat) windowLocked(now time.Time, enc *encounter, current bool) (bool, float64) {
	active := false
	if current && !enc.closed {
		active = now.Sub(enc.last) < idleTimeout
		if enc.boss {
			active = now.Sub(enc.last) < bossGrace
		}
	}
	elapsed := enc.last.Sub(enc.start).Seconds()
	if elapsed < 0.001 {
		elapsed = 0.001
	}
	return active, elapsed
}

// summaryLocked construye una entrada del historial. Requiere c.mu.
func (c *Combat) summaryLocked(now time.Time, enc *encounter) EncounterSummary {
	_, elapsed := c.windowLocked(now, enc, false)
	s := EncounterSummary{
		ID:           enc.id,
		Title:        "Fight",
		IsBoss:       enc.boss,
		Elapsed:      elapsed,
		TotalDamage:  sumValues(enc.damage),
		TotalHealing: sumValues(enc.healing),
		TotalTaken:   sumValues(enc.taken),
	}
	if !enc.start.IsZero() {
		s.StartedAt = enc.start.UnixMilli()
	}
	if enc.boss && enc.mainTarget > 0 {
		s.BossID = enc.mainTarget
		name := ""
		if e := c.entsFor(enc)[enc.mainTarget]; e != nil {
			name = e.Name
		}
		if name == "" {
			name = "Jefe #" + itoa(enc.mainTarget)
		}
		s.Boss = name
		s.Title = name
	}
	return s
}

// sumValues suma los valores de un mapa de acumuladores.
func sumValues(values map[int]int64) int64 {
	var total int64
	for _, v := range values {
		total += v
	}
	return total
}

// MemberDetail devuelve el desglose de daño del encuentro actual.
func (c *Combat) MemberDetail(now time.Time, key string) MemberDetail {
	return c.MemberDetailFor(now, 0, key, MetricDamage)
}

// MemberDetailFor devuelve el desglose por habilidad de la fila canónica key
// para la métrica y el encuentro indicados. id <= 0 usa el encuentro actual.
func (c *Combat) MemberDetailFor(now time.Time, id int, key string, metric Metric) MemberDetail {
	c.mu.Lock()
	defer c.mu.Unlock()
	if metric != MetricHealing && metric != MetricTaken {
		metric = MetricDamage
	}
	detail := MemberDetail{
		Key:    key,
		Metric: metric,
		Active: false,
		Skills: make([]SkillStat, 0),
	}
	enc := c.encounterLocked(id)
	if enc == nil {
		return detail
	}
	detail.Active, detail.Elapsed = c.windowLocked(now, enc, enc == c.cur)
	ents := c.entsFor(enc)

	var rows map[string]*aggRow
	var breakdown map[hitKey]*hitAcc
	switch metric {
	case MetricHealing:
		rows = c.rowsFromLocked(ents, enc.healing, enc.healHits, enc.healCrits, false)
		breakdown = enc.healBreakdown
	case MetricTaken:
		rows = c.rowsFromLocked(ents, enc.taken, enc.takenHits, enc.takenCrits, true)
		breakdown = enc.takenBreakdown
	default:
		rows = c.rowsFromLocked(ents, enc.damage, enc.hits, enc.crits, false)
		breakdown = enc.breakdown
	}

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
	detail.DPS = float64(r.damage) / detail.Elapsed
	detail.Hits = r.hits
	detail.Crits = r.crits

	// En daño recibido se agrupa por fuente y habilidad; en daño y curación
	// basta con la habilidad porque el miembro ya es el actor.
	type skillKey struct {
		id    int
		actor int
	}
	bySkill := map[skillKey]*hitAcc{}
	for hk, acc := range breakdown {
		var memberKey string
		if metric == MetricTaken {
			memberKey, _ = victimKey(ents, hk.target)
		} else {
			memberKey, _ = actorKey(ents, ents[hk.actor], hk.actor)
		}
		if memberKey != key {
			continue
		}
		sk := skillKey{id: hk.skill}
		if metric == MetricTaken {
			sk.actor = hk.actor
		}
		dst := bySkill[sk]
		if dst == nil {
			dst = &hitAcc{}
			bySkill[sk] = dst
		}
		dst.add(acc)
	}

	detail.Skills = make([]SkillStat, 0, len(bySkill))
	for sk, acc := range bySkill {
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
		stat := SkillStat{
			ID:        sk.id,
			Key:       itoa(sk.id),
			Name:      SkillName(sk.id),
			Icon:      SkillIcon(sk.id),
			Damage:    acc.damage,
			DPS:       float64(acc.damage) / detail.Elapsed,
			Share:     share,
			Hits:      acc.hits,
			ExtraHits: acc.extraHits,
			TotalHits: totalHits,
			Crits:     acc.crits,
			AvgHit:    avgHit,
		}
		if metric == MetricTaken {
			stat.Key = itoa(sk.actor) + ":" + itoa(sk.id)
			_, stat.Source = actorKey(ents, ents[sk.actor], sk.actor)
		}
		detail.Skills = append(detail.Skills, stat)
	}
	sort.Slice(detail.Skills, func(i, j int) bool {
		if detail.Skills[i].Damage != detail.Skills[j].Damage {
			return detail.Skills[i].Damage > detail.Skills[j].Damage
		}
		if detail.Skills[i].Name != detail.Skills[j].Name {
			return detail.Skills[i].Name < detail.Skills[j].Name
		}
		if detail.Skills[i].ID != detail.Skills[j].ID {
			return detail.Skills[i].ID < detail.Skills[j].ID
		}
		return detail.Skills[i].Key < detail.Skills[j].Key
	})
	return detail
}

// rowsFromLocked agrega un mapa de valores por entidad a filas canónicas.
// resolveVictim indica que los ids son víctimas y las mascotas se suman a su
// dueño. Requiere c.mu.
func (c *Combat) rowsFromLocked(ents map[int]*Entity, values map[int]int64, hits, crits map[int]int, resolveVictim bool) map[string]*aggRow {
	rows := map[string]*aggRow{}
	for id, v := range values {
		if v <= 0 {
			continue
		}
		e := ents[id]
		key, name := metricKey(ents, e, id, resolveVictim)
		r := rows[key]
		if r == nil {
			r = &aggRow{key: key, id: id, name: name}
			rows[key] = r
		}
		r.damage += v
		r.hits += hits[id]
		r.crits += crits[id]
		if e == nil {
			continue
		}
		if resolveVictim && e.Owner > 0 {
			if owner := ents[e.Owner]; owner != nil {
				e = owner
			}
		}
		if e.Self {
			r.self = true
		}
		// Se prefiere el id y la clase de una entidad jugador real antes que los
		// de uno de sus summons.
		if realPlayer := e.IsPlayer && !e.NPC; realPlayer && !r.player {
			r.player = true
			r.id = e.ID
			if e.Class != 0 {
				r.class = e.Class
			}
		} else if r.class == 0 && e.Class != 0 {
			r.class = e.Class
		}
	}
	return rows
}

// metricKey resuelve la clave canónica de una entidad contra el mapa ents. Para
// daño recibido las mascotas se suman a su dueño.
func metricKey(ents map[int]*Entity, e *Entity, id int, resolveVictim bool) (key, name string) {
	if resolveVictim && e != nil && e.Owner > 0 {
		if owner := ents[e.Owner]; owner != nil {
			e = owner
			id = owner.ID
		}
	}
	return actorKey(ents, e, id)
}

// victimKey resuelve la víctima de un golpe a su dueño si es una mascota.
func victimKey(ents map[int]*Entity, id int) (key, name string) {
	return metricKey(ents, ents[id], id, true)
}

// metricStatsLocked construye y ordena una tabla de curación o daño recibido.
// Requiere c.mu.
func (c *Combat) metricStatsLocked(ents map[int]*Entity, values map[int]int64, hits, crits map[int]int, resolveVictim bool, elapsed float64) []MetricStat {
	rows := c.rowsFromLocked(ents, values, hits, crits, resolveVictim)
	out := make([]MetricStat, 0, len(rows))
	for _, r := range rows {
		cls, color := ClassName(r.class)
		if cls == "" {
			cls = "?"
		}
		out = append(out, MetricStat{
			ID:        r.id,
			Key:       r.key,
			Name:      r.name,
			Class:     cls,
			Color:     color,
			Value:     r.damage,
			PerSecond: float64(r.damage) / elapsed,
			Hits:      r.hits,
			Crits:     r.crits,
			Self:      r.self,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Value != out[j].Value {
			return out[i].Value > out[j].Value
		}
		return out[i].Name < out[j].Name
	})
	return out
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

// actorKey asigna un actor a su fila canónica en la tabla. Los summons y
// hechizos de suelo (entidades NPC con el nombre de su dueño) resuelven al
// nombre del dueño; las entidades sin nombre caen a su id.
func actorKey(ents map[int]*Entity, e *Entity, id int) (key, name string) {
	if e != nil {
		if e.Name != "" {
			return "n:" + e.Name, e.Name
		}
		if e.Owner > 0 {
			if o := ents[e.Owner]; o != nil && o.Name != "" {
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
