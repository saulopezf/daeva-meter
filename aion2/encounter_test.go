package aion2

import (
	"testing"
	"time"
)

func TestClassName(t *testing.T) {
	cases := map[int]string{
		5: "Gladiator", 8: "Gladiator",
		9: "Templar", 11: "Templar", 12: "Templar",
		13: "Ranger", 17: "Assassin",
		21: "Elementalist", 24: "Elementalist",
		25: "Sorcerer", 28: "Sorcerer",
		29: "Cleric", 33: "Chanter", 36: "Chanter",
		0: "", 4: "", 37: "",
	}
	for id, want := range cases {
		got, _ := ClassName(id)
		if got != want {
			t.Errorf("ClassName(%d) = %q, want %q", id, got, want)
		}
	}
}

func TestEncounterMergesCloseHits(t *testing.T) {
	c := NewCombat()
	c.SetSelfID(100)
	t0 := time.Unix(1000, 0)
	c.AddHit(t0, Hit{Actor: 100, Target: 200, Damage: 100, HitType: 1})
	c.AddHit(t0.Add(2*time.Second), Hit{Actor: 100, Target: 200, Damage: 50, HitType: 1})

	snap := c.Snapshot(t0.Add(2 * time.Second))
	if len(snap.Members) != 1 || snap.Members[0].Damage != 150 {
		t.Fatalf("members = %+v, want one with 150", snap.Members)
	}
	if !snap.Active {
		t.Errorf("encounter should be active")
	}
}

func TestEncounterSplitsAfterIdle(t *testing.T) {
	c := NewCombat()
	c.SetSelfID(100)
	t0 := time.Unix(1000, 0)
	c.AddHit(t0, Hit{Actor: 100, Target: 200, Damage: 100, HitType: 1})
	c.AddHit(t0.Add(6*time.Second), Hit{Actor: 100, Target: 300, Damage: 50, HitType: 1})

	snap := c.Snapshot(t0.Add(6 * time.Second))
	if len(snap.Members) != 1 || snap.Members[0].Damage != 50 {
		t.Fatalf("members = %+v, want one with 50 (new encounter)", snap.Members)
	}
}

func TestEncounterFreezes(t *testing.T) {
	c := NewCombat()
	c.SetSelfID(100)
	t0 := time.Unix(1000, 0)
	c.AddHit(t0, Hit{Actor: 100, Target: 200, Damage: 100, HitType: 1})

	snap := c.Snapshot(t0.Add(10 * time.Second))
	if snap.Active {
		t.Errorf("encounter should be frozen after idle")
	}
	if len(snap.Members) != 1 || snap.Members[0].Damage != 100 {
		t.Fatalf("frozen members = %+v, want one with 100", snap.Members)
	}
}

func TestNonGroupActorIgnored(t *testing.T) {
	c := NewCombat()
	c.SetSelfID(100)
	t0 := time.Unix(1000, 0)
	c.AddHit(t0, Hit{Actor: 999, Target: 200, Damage: 500, HitType: 1})

	if snap := c.Snapshot(t0); len(snap.Members) != 0 {
		t.Fatalf("members = %+v, want none (actor 999 is not in the group)", snap.Members)
	}
}

func TestPartyMemberCounted(t *testing.T) {
	c := NewCombat()
	c.SetSelfID(100)
	c.SetParty([]rosterMember{{Name: "Buddy", Class: 11, Level: 40}})
	c.SetIdentity(300, "Buddy", false, 0, 0, 0)

	t0 := time.Unix(1000, 0)
	c.AddHit(t0, Hit{Actor: 300, Target: 200, Damage: 700, HitType: 1})

	snap := c.Snapshot(t0)
	if len(snap.Party) != 1 || snap.Party[0] != "Buddy" {
		t.Fatalf("party = %v, want [Buddy]", snap.Party)
	}
	if len(snap.Members) != 1 || snap.Members[0].Name != "Buddy" || snap.Members[0].Damage != 700 {
		t.Fatalf("members = %+v, want Buddy with 700", snap.Members)
	}
	if cls, _ := ClassName(300); cls == "" {
		_ = cls
	}
}

func TestPartyResolvesNameBeforeIdentity(t *testing.T) {
	// Identity arrives before the roster: the roster must still link the name.
	c := NewCombat()
	c.SetSelfID(100)
	c.SetIdentity(300, "Buddy", false, 0, 0, 0)
	c.SetParty([]rosterMember{{Name: "Buddy", Class: 11, Level: 40}})

	t0 := time.Unix(1000, 0)
	c.AddHit(t0, Hit{Actor: 300, Target: 200, Damage: 700, HitType: 1})
	if snap := c.Snapshot(t0); len(snap.Members) != 1 {
		t.Fatalf("members = %+v, want Buddy", snap.Members)
	}
}

func TestClearParty(t *testing.T) {
	c := NewCombat()
	c.SetSelfID(100)
	c.SetParty([]rosterMember{{Name: "Buddy", Class: 11, Level: 40}})
	c.SetIdentity(300, "Buddy", false, 0, 0, 0)
	c.ClearParty()

	if snap := c.Snapshot(time.Now()); len(snap.Party) != 0 {
		t.Fatalf("party = %v, want empty", snap.Party)
	}
	t0 := time.Unix(1000, 0)
	c.AddHit(t0, Hit{Actor: 300, Target: 200, Damage: 700, HitType: 1})
	if snap := c.Snapshot(t0); len(snap.Members) != 0 {
		t.Fatalf("members = %+v, want none after leaving the group", snap.Members)
	}
}

func TestUnknownSelfIsPermissive(t *testing.T) {
	// Before the local id is known we keep data rather than showing nothing.
	c := NewCombat()
	t0 := time.Unix(1000, 0)
	c.AddHit(t0, Hit{Actor: 999, Target: 200, Damage: 500, HitType: 1})
	if snap := c.Snapshot(t0); len(snap.Members) != 1 {
		t.Fatalf("members = %+v, want one while self is unknown", snap.Members)
	}
	// Once self is known, the pre-identity data is discarded.
	c.SetSelfID(100)
	if snap := c.Snapshot(t0); len(snap.Members) != 0 {
		t.Fatalf("members = %+v, want none after self is resolved", snap.Members)
	}
}

func TestSummonMergedIntoOwner(t *testing.T) {
	// A pet/ground spell is an NPC entity that carries the owner's name; its
	// damage must roll into the owner's single row.
	c := NewCombat()
	c.SetSelfID(100)
	c.SetParty([]rosterMember{{Name: "Hero", Class: 25, Level: 33}})
	c.SetIdentity(100, "Hero", true, 0, 25, 33)
	c.SetName(200, "Hero") // summon named after its owner
	c.MarkNPC(200, 0)

	t0 := time.Unix(1000, 0)
	c.AddHit(t0, Hit{Actor: 100, Target: 900, Damage: 100, HitType: 1})
	c.AddHit(t0, Hit{Actor: 200, Target: 900, Damage: 50, HitType: 1})

	snap := c.Snapshot(t0)
	if len(snap.Members) != 1 {
		t.Fatalf("members = %+v, want a single merged row", snap.Members)
	}
	m := snap.Members[0]
	if m.Name != "Hero" || m.Damage != 150 || !m.Self || m.ID != 100 {
		t.Fatalf("row = %+v, want Hero/150/self/#100", m)
	}
}

func TestMultiplePlayerIDsMerged(t *testing.T) {
	// The same player can hold several entity ids (zone reload reassigns them);
	// the board must show one row.
	c := NewCombat()
	c.SetSelfID(100)
	c.SetParty([]rosterMember{{Name: "Ally", Class: 11, Level: 40}})
	c.SetIdentity(300, "Ally", false, 0, 11, 40)
	c.SetIdentity(400, "Ally", false, 0, 11, 40)

	t0 := time.Unix(1000, 0)
	c.AddHit(t0, Hit{Actor: 300, Target: 900, Damage: 100, HitType: 1})
	c.AddHit(t0, Hit{Actor: 400, Target: 900, Damage: 70, HitType: 1})

	snap := c.Snapshot(t0)
	if len(snap.Members) != 1 {
		t.Fatalf("members = %+v, want a single merged row", snap.Members)
	}
	if m := snap.Members[0]; m.Name != "Ally" || m.Damage != 170 {
		t.Fatalf("row = %+v, want Ally/170", m)
	}
}

func TestSummonAttributedByOwner(t *testing.T) {
	// A summon with no inline name is attributed through its owner id.
	c := NewCombat()
	c.SetSelfID(100)
	c.SetParty([]rosterMember{{Name: "Ally", Class: 11, Level: 40}})
	c.SetIdentity(300, "Ally", false, 0, 11, 40)
	c.MarkNPC(500, 300) // summon owned by Ally, no name of its own

	t0 := time.Unix(1000, 0)
	c.AddHit(t0, Hit{Actor: 300, Target: 900, Damage: 100, HitType: 1})
	c.AddHit(t0, Hit{Actor: 500, Target: 900, Damage: 40, HitType: 1})

	snap := c.Snapshot(t0)
	if len(snap.Members) != 1 {
		t.Fatalf("members = %+v, want a single merged row", snap.Members)
	}
	if m := snap.Members[0]; m.Name != "Ally" || m.Damage != 140 {
		t.Fatalf("row = %+v, want Ally/140", m)
	}
}

func TestForeignSummonIgnored(t *testing.T) {
	// A pet owned by a player outside the group must not be counted.
	c := NewCombat()
	c.SetSelfID(100)
	c.MarkNPC(500, 999) // owner 999 is not in the group

	t0 := time.Unix(1000, 0)
	c.AddHit(t0, Hit{Actor: 500, Target: 900, Damage: 40, HitType: 1})
	if snap := c.Snapshot(t0); len(snap.Members) != 0 {
		t.Fatalf("members = %+v, want none for a foreign pet", snap.Members)
	}
}
