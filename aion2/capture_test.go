package aion2

import (
	"bufio"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// captureLine reconoce una línea del log de sesión:
//
//	+12.345 S2C stream=1 len=42 aabbcc...
var captureLine = regexp.MustCompile(`^\+([0-9.]+) (S2C|C2S) stream=(\d+) len=(\d+) ([0-9a-f]+)$`)

// feedCapture pasa un log de sesión por el parser exactamente como lo hace el
// pipeline en vivo (un buffer por stream TCP, en orden de captura). Si onSnapshot
// está definido, se llama tras cada segmento alimentado con la tabla actual.
func feedCapture(t *testing.T, path string, onSnapshot func(sec float64, snap Snapshot)) (*Combat, Snapshot) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open capture: %v", err)
	}
	defer f.Close()

	combat := NewCombat()
	parser := NewParser(combat)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var maxSec float64

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 8<<20)
	for sc.Scan() {
		m := captureLine.FindStringSubmatch(sc.Text())
		if m == nil || m[2] != "S2C" {
			continue
		}
		sec, _ := strconv.ParseFloat(m[1], 64)
		if sec > maxSec {
			maxSec = sec
		}
		stream, _ := strconv.ParseInt(m[3], 10, 64)
		data, err := hex.DecodeString(m[5])
		if err != nil {
			continue
		}
		ts := base.Add(time.Duration(sec * float64(time.Second)))
		parser.Feed(stream, data, ts)
		if onSnapshot != nil {
			onSnapshot(sec, combat.Snapshot(ts))
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan capture: %v", err)
	}
	return combat, combat.Snapshot(base.Add(time.Duration(maxSec * float64(time.Second))))
}

// replayCapture alimenta un log de sesión y devuelve la tabla final.
func replayCapture(t *testing.T, path string) (*Combat, Snapshot) {
	t.Helper()
	return feedCapture(t, path, nil)
}

func capturePath(name string) string {
	return filepath.Join("..", "captures", name)
}

func findEntity(t *testing.T, c *Combat, id int) Entity {
	t.Helper()
	for _, e := range c.Entities() {
		if e.ID == id {
			return e
		}
	}
	t.Fatalf("entity #%d not found", id)
	return Entity{}
}

// TestCaptureTemplar fija una pelea en solitario: un único encuentro con el daño
// solo del personaje local. El nombre del personaje se lee de la captura en vez
// de estar fijado, para no dejar ningún nombre real de jugador en el código.
func TestCaptureTemplar(t *testing.T) {
	path := capturePath("session-20261016-215243.txt")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("capture not present: %v", err)
	}

	combat, snap := replayCapture(t, path)

	if snap.SelfID != 15664 || snap.Self == "" {
		t.Errorf("self = #%d %q, want #15664 with a name", snap.SelfID, snap.Self)
	}
	self := findEntity(t, combat, 15664)
	if cls, _ := ClassName(self.Class); self.Class != 11 || cls != "Templar" {
		t.Errorf("self class = %d (%s), want 11 Templar", self.Class, cls)
	}
	if self.Level != 45 {
		t.Errorf("self level = %d, want 45", self.Level)
	}
	if len(snap.Party) != 0 {
		t.Errorf("party = %v, want empty", snap.Party)
	}
	if len(snap.Members) != 1 {
		t.Fatalf("members = %+v, want exactly the player", snap.Members)
	}
	m := snap.Members[0]
	if m.Name != snap.Self || !m.Self {
		t.Errorf("member = %+v, want the local player", m)
	}
	if m.Class != "Templar" {
		t.Errorf("member class = %q, want Templar", m.Class)
	}
	if m.Damage != 666964 || m.Hits != 89 || m.Crits != 25 {
		t.Errorf("damage/hits/crits = %d/%d/%d, want 666964/89/25", m.Damage, m.Hits, m.Crits)
	}
}

// TestCaptureSorcererNoBystanders fija la regresión clave: en una zona poblada la
// tabla muestra solo al jugador local, nunca el daño de otros jugadores.
func TestCaptureSorcererNoBystanders(t *testing.T) {
	path := capturePath("session-20261016-221433.txt")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("capture not present: %v", err)
	}

	combat, snap := replayCapture(t, path)

	if snap.Self == "" {
		t.Fatalf("self has no name: %+v", snap)
	}
	self := findEntity(t, combat, snap.SelfID)
	if cls, _ := ClassName(self.Class); self.Class < 25 || self.Class > 28 || cls != "Sorcerer" {
		t.Errorf("self class = %d (%s), want a Sorcerer (25-28)", self.Class, cls)
	}
	if len(snap.Party) != 0 {
		t.Errorf("party = %v, want empty", snap.Party)
	}
	if len(snap.Members) != 1 {
		t.Fatalf("members = %+v, want only the local player", snap.Members)
	}
	if snap.Members[0].Name != snap.Self {
		t.Errorf("member = %q, want the local player %q", snap.Members[0].Name, snap.Self)
	}

	// Otros jugadores vistos vía 45 36 no deben aparecer en la tabla.
	board := map[int]bool{}
	for _, m := range snap.Members {
		board[m.ID] = true
	}
	for _, e := range combat.Entities() {
		if e.Name == "" || e.Self || e.NPC {
			continue
		}
		if board[e.ID] {
			t.Errorf("bystander %q (#%d) leaked into the DPS board", e.Name, e.ID)
		}
	}
}

// TestCaptureGroupNoDuplicateNames reproduce una incursión a mazmorra y comprueba
// que la tabla nunca lista dos veces al mismo jugador, aunque las mascotas y los
// hechizos de suelo peleen bajo el nombre de su dueño y el juego reasigne ids de
// entidad al cargar zona. Los nombres del roster se derivan de la captura, no
// están fijados.
func TestCaptureGroupNoDuplicateNames(t *testing.T) {
	path := capturePath("session-20261017-015343.txt")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("capture not present: %v", err)
	}

	const wantPartySize = 5
	maxNamed := 0

	combat, snap := feedCapture(t, path, func(sec float64, s Snapshot) {
		seen := map[string]int{}
		for _, m := range s.Members {
			if m.Name == "" || strings.HasPrefix(m.Name, "#") {
				continue
			}
			seen[m.Name]++
		}
		for name, n := range seen {
			if n > 1 {
				t.Errorf("duplicate name %q (%d rows) at t=%.1fs", name, n, sec)
			}
		}
		if len(seen) > maxNamed {
			maxNamed = len(seen)
		}
	})

	if len(snap.Party) != wantPartySize {
		t.Errorf("party size = %d (%v), want %d", len(snap.Party), snap.Party, wantPartySize)
	}
	if snap.Self == "" {
		t.Fatal("self has no name")
	}
	if !contains(snap.Party, snap.Self) {
		t.Errorf("self %q not in party %v", snap.Self, snap.Party)
	}
	// Cada nombre del roster debe pertenecer a una entidad jugador real vista.
	players := map[string]bool{}
	for _, e := range combat.Entities() {
		if e.Name != "" && (e.IsPlayer || e.Self) {
			players[e.Name] = true
		}
	}
	for _, name := range snap.Party {
		if !players[name] {
			t.Errorf("roster name %q has no player entity", name)
		}
	}
	if maxNamed > wantPartySize {
		t.Errorf("board showed %d named rows, more than the %d party members", maxNamed, wantPartySize)
	}
	for _, m := range snap.Members {
		if m.Name == "" {
			t.Errorf("board row without a name: %+v", m)
		}
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
