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

// captureLine matches one line of a session log:
//
//	+12.345 S2C stream=1 len=42 aabbcc...
var captureLine = regexp.MustCompile(`^\+([0-9.]+) (S2C|C2S) stream=(\d+) len=(\d+) ([0-9a-f]+)$`)

// feedCapture feeds a session log through the parser exactly like the live
// pipeline does (one buffer per TCP stream, in capture order). When onSnapshot
// is set it is called after every fed segment with the current board.
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

// replayCapture feeds a session log and returns the final board.
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

// TestCaptureTemplar pins a solo fight: a single encounter with the local
// character's own damage only. The character name is read from the capture
// rather than hardcoded, so no real player name lives in the source.
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

// TestCaptureSorcererNoBystanders pins the key regression: in a crowded zone
// the board shows only the local player, never the other players' damage.
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

	// Other players seen via 45 36 must not appear on the board.
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

// TestCaptureGroupNoDuplicateNames replays a dungeon run and checks that the
// board never lists the same player twice, even though pets and ground spells
// fight under their owner's name and the game reassigns entity ids on a zone
// load. Roster names are derived from the capture, not hardcoded.
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
	// Every roster name must belong to a real player entity we saw.
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
