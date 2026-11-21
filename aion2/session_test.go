package aion2

import (
	"testing"
	"time"
)

// TestSnapshotHealingAndTaken comprueba que curación y daño recibido se separan
// del daño infligido y que el detalle por habilidad usa la métrica correcta.
func TestSnapshotHealingAndTaken(t *testing.T) {
	c := NewCombat()
	c.SetSelfID(100)
	c.SetIdentity(100, "Hero", true, 0, 29, 40)
	c.SetParty([]rosterMember{{Name: "Tank", Class: 11, Level: 40}})
	c.SetIdentity(200, "Tank", false, 0, 11, 40)
	c.MarkNPC(900, 0)

	t0 := time.Unix(1000, 0)
	c.AddHit(t0, Hit{Actor: 100, Target: 900, Skill: 111, Damage: 100, HitType: 1})
	c.AddHit(t0.Add(time.Second), Hit{Actor: 100, Target: 900, Skill: 111, Damage: 100, HitType: 1})
	c.AddHit(t0.Add(time.Second), Hit{Actor: 100, Target: 200, Skill: 222, Heal: 50, HitType: 1})
	c.AddHit(t0.Add(time.Second), Hit{Actor: 900, Target: 200, Skill: 1_234_567, Damage: 30, HitType: 1, NPCSkill: true})

	now := t0.Add(time.Second)
	snap := c.Snapshot(now)
	if snap.ID == 0 {
		t.Fatal("snapshot has no encounter id")
	}
	if len(snap.Members) != 1 || snap.Members[0].Damage != 200 {
		t.Fatalf("damage members = %+v, want one with 200", snap.Members)
	}
	if len(snap.Healing) != 1 {
		t.Fatalf("healing = %+v, want one row", snap.Healing)
	}
	h := snap.Healing[0]
	if h.Name != "Hero" || h.Value != 50 || h.PerSecond != 50 {
		t.Errorf("healing row = %+v, want Hero 50 (50/s)", h)
	}
	if len(snap.Taken) != 1 {
		t.Fatalf("taken = %+v, want one row", snap.Taken)
	}
	tk := snap.Taken[0]
	if tk.Name != "Tank" || tk.Value != 30 || tk.PerSecond != 30 {
		t.Errorf("taken row = %+v, want Tank 30 (30/s)", tk)
	}

	hd := c.MemberDetailFor(now, 0, "n:Hero", MetricHealing)
	if !hd.Found || hd.Metric != MetricHealing || hd.Damage != 50 || len(hd.Skills) != 1 {
		t.Fatalf("healing detail = %+v, want found Hero with one skill", hd)
	}
	if hd.Skills[0].ID != 222 || hd.Skills[0].Damage != 50 {
		t.Errorf("healing skill = %+v, want id 222 with 50", hd.Skills[0])
	}

	td := c.MemberDetailFor(now, 0, "n:Tank", MetricTaken)
	if !td.Found || td.Metric != MetricTaken || td.Damage != 30 || len(td.Skills) != 1 {
		t.Fatalf("taken detail = %+v, want found Tank with one skill", td)
	}
	if td.Skills[0].ID != 1_234_567 || td.Skills[0].Damage != 30 {
		t.Errorf("taken skill = %+v, want id 1234567 with 30", td.Skills[0])
	}
}

// TestHistoryArchivesAndRestores comprueba que al empezar un combate nuevo el
// anterior pasa al historial y se puede consultar por id.
func TestHistoryArchivesAndRestores(t *testing.T) {
	c := NewCombat()
	c.SetSelfID(100)
	t0 := time.Unix(1000, 0)
	c.AddHit(t0, Hit{Actor: 100, Target: 200, Damage: 100, HitType: 1})
	first := c.Snapshot(t0)

	c.AddHit(t0.Add(40*time.Second), Hit{Actor: 100, Target: 300, Damage: 50, HitType: 1})
	now := t0.Add(40 * time.Second)
	second := c.Snapshot(now)

	hist := c.History(now)
	if len(hist) != 1 {
		t.Fatalf("history = %+v, want one archived encounter", hist)
	}
	if hist[0].ID != first.ID || hist[0].TotalDamage != 100 {
		t.Errorf("history entry = %+v, want first encounter with 100", hist[0])
	}
	if second.ID == first.ID {
		t.Errorf("second encounter reused id %d", first.ID)
	}

	old := c.SnapshotFor(now, first.ID)
	if old.ID != first.ID || old.Active {
		t.Errorf("archived snapshot = %+v, want id %d and inactive", old, first.ID)
	}
	if len(old.Members) != 1 || old.Members[0].Damage != 100 {
		t.Errorf("archived members = %+v, want one with 100", old.Members)
	}

	md := c.MemberDetailFor(now, first.ID, "i:100", MetricDamage)
	if !md.Found || md.Damage != 100 {
		t.Errorf("archived detail = %+v, want 100 damage", md)
	}
}

// TestHealingDoesNotMergeSeparateFights comprueba que una curación mantiene
// vivo el encuentro pero el siguiente daño sigue partiendo por el hueco de daño.
func TestHealingDoesNotMergeSeparateFights(t *testing.T) {
	c := NewCombat()
	c.SetSelfID(100)
	t0 := time.Unix(1000, 0)
	c.AddHit(t0, Hit{Actor: 100, Target: 200, Damage: 100, HitType: 1})
	c.AddHit(t0.Add(3*time.Second), Hit{Actor: 100, Target: 100, Skill: 222, Heal: 50, HitType: 1})
	c.AddHit(t0.Add(7*time.Second), Hit{Actor: 100, Target: 300, Damage: 50, HitType: 1})

	now := t0.Add(7 * time.Second)
	snap := c.Snapshot(now)
	if len(snap.Members) != 1 || snap.Members[0].Damage != 50 {
		t.Fatalf("current members = %+v, want one with 50", snap.Members)
	}
	hist := c.History(now)
	if len(hist) != 1 {
		t.Fatalf("history = %+v, want one archived encounter", hist)
	}
	if hist[0].TotalDamage != 100 || hist[0].TotalHealing != 50 {
		t.Errorf("history entry = %+v, want 100 damage and 50 healing", hist[0])
	}
}

// TestDamageTakenIgnoresBystanders comprueba que el daño recibido por un jugador
// ajeno al grupo no aparece en la tabla.
func TestDamageTakenIgnoresBystanders(t *testing.T) {
	c := NewCombat()
	c.SetSelfID(100)
	c.SetIdentity(100, "Hero", true, 0, 29, 40)
	c.SetIdentity(300, "Bystander", false, 0, 21, 40)
	c.MarkNPC(900, 0)

	t0 := time.Unix(1000, 0)
	c.AddHit(t0, Hit{Actor: 100, Target: 900, Damage: 100, HitType: 1})
	c.AddHit(t0, Hit{Actor: 900, Target: 300, Damage: 30, HitType: 1, NPCSkill: true})

	snap := c.Snapshot(t0)
	if len(snap.Taken) != 0 {
		t.Fatalf("taken = %+v, want no rows for bystanders", snap.Taken)
	}
}

// TestHealingWithoutEncounterIsIgnored documenta que el combate lo abre el daño
// infligido: una curación aislada no crea un encuentro.
func TestHealingWithoutEncounterIsIgnored(t *testing.T) {
	c := NewCombat()
	c.SetSelfID(100)
	t0 := time.Unix(1000, 0)
	c.AddHit(t0, Hit{Actor: 100, Target: 100, Skill: 222, Heal: 50, HitType: 1})

	snap := c.Snapshot(t0)
	if snap.ID != 0 || len(snap.Healing) != 0 {
		t.Fatalf("snapshot = %+v, want no encounter and no healing", snap)
	}
}

// TestResetEncounterArchivesCurrent comprueba que un cambio de zona archiva el
// combate en curso en lugar de perderlo.
func TestResetEncounterArchivesCurrent(t *testing.T) {
	c := NewCombat()
	c.SetSelfID(100)
	t0 := time.Unix(1000, 0)
	c.AddHit(t0, Hit{Actor: 100, Target: 200, Damage: 100, HitType: 1})
	c.ResetEncounter()

	if snap := c.Snapshot(t0); snap.ID != 0 || len(snap.Members) != 0 {
		t.Fatalf("snapshot after reset = %+v, want empty current encounter", snap)
	}
	hist := c.History(t0)
	if len(hist) != 1 || hist[0].TotalDamage != 100 {
		t.Fatalf("history after reset = %+v, want one fight with 100 damage", hist)
	}
}

// TestCloseEncounterSplitsNextFight comprueba que detener la captura cierra el
// combate actual sin borrarlo y que el siguiente golpe abre uno nuevo.
func TestCloseEncounterSplitsNextFight(t *testing.T) {
	c := NewCombat()
	c.SetSelfID(100)
	t0 := time.Unix(1000, 0)
	c.AddHit(t0, Hit{Actor: 100, Target: 200, Damage: 100, HitType: 1})
	c.CloseEncounter()

	snap := c.Snapshot(t0.Add(time.Second))
	if snap.Active {
		t.Errorf("closed encounter should not be active")
	}

	c.AddHit(t0.Add(2*time.Second), Hit{Actor: 100, Target: 200, Damage: 50, HitType: 1})
	now := t0.Add(2 * time.Second)
	snap = c.Snapshot(now)
	if len(snap.Members) != 1 || snap.Members[0].Damage != 50 {
		t.Fatalf("current members = %+v, want one with 50", snap.Members)
	}
	if len(c.History(now)) != 1 {
		t.Fatalf("history = %+v, want the closed encounter archived", c.History(now))
	}
}

// TestArchivedEncounterKeepsIdentities comprueba que un combate archivado
// conserva nombres y jefe aunque el juego reasigne los ids tras cambiar de zona.
func TestArchivedEncounterKeepsIdentities(t *testing.T) {
	c := NewCombat()
	c.SetSelfID(100)
	c.SetIdentity(100, "Hero", true, 0, 29, 40)
	c.MarkNPC(900, 0)
	c.SetName(900, "Dragon")
	c.MarkBoss(900)

	t0 := time.Unix(1000, 0)
	c.AddHit(t0, Hit{Actor: 100, Target: 900, Skill: 111, Damage: 100, HitType: 1})
	first := c.Snapshot(t0)
	if !first.IsBoss || first.Boss != "Dragon" {
		t.Fatalf("live snapshot = %+v, want boss Dragon", first)
	}
	c.ResetEncounter()

	// Tras el cambio de zona, los mismos ids pasan a otras entidades.
	c.SetIdentity(100, "Other", true, 0, 11, 40)
	c.SetName(900, "Goblin")

	now := t0.Add(time.Minute)
	old := c.SnapshotFor(now, first.ID)
	if old.Boss != "Dragon" {
		t.Errorf("archived boss = %q, want Dragon", old.Boss)
	}
	if len(old.Members) != 1 || old.Members[0].Name != "Hero" {
		t.Fatalf("archived members = %+v, want Hero", old.Members)
	}
	hist := c.History(now)
	if len(hist) != 1 || hist[0].Boss != "Dragon" {
		t.Errorf("history = %+v, want Dragon", hist)
	}
	md := c.MemberDetailFor(now, first.ID, "n:Hero", MetricDamage)
	if !md.Found || md.Damage != 100 || len(md.Skills) != 1 {
		t.Errorf("archived detail = %+v, want Hero with 100", md)
	}
}
