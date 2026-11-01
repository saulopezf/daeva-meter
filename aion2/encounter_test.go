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

// TestEncounterMergesCloseHits verifica que dos golpes separados menos que
// idleTimeout se suman en el mismo encuentro.
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

// TestEncounterSplitsAfterIdle verifica que un golpe tras más de idleTimeout
// inicia un encuentro nuevo (solo cuenta el daño nuevo).
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

// TestEncounterFreezes verifica que un encuentro sin actividad reciente queda
// congelado (inactivo) pero sigue visible.
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

// TestEncounterPhaseGraceSameTarget verifica que un hueco de fase sobre el
// mismo objetivo no parte el encuentro.
func TestEncounterPhaseGraceSameTarget(t *testing.T) {
	c := NewCombat()
	c.SetSelfID(100)
	t0 := time.Unix(1000, 0)
	c.AddHit(t0, Hit{Actor: 100, Target: 200, Damage: 100, HitType: 1})
	c.AddHit(t0.Add(20*time.Second), Hit{Actor: 100, Target: 200, Damage: 50, HitType: 1})

	snap := c.Snapshot(t0.Add(20 * time.Second))
	if len(snap.Members) != 1 || snap.Members[0].Damage != 150 {
		t.Fatalf("members = %+v, want one with 150", snap.Members)
	}
	if snap.Elapsed < 19.9 || snap.Elapsed > 20.1 {
		t.Errorf("elapsed = %.1f, want ~20", snap.Elapsed)
	}
}

// TestEncounterSplitsNewTargetAfterGrace verifica que un enemigo distinto
// después del timeout abre un encuentro nuevo aunque el anterior siga dentro de
// phaseGrace.
func TestEncounterSplitsNewTargetAfterGrace(t *testing.T) {
	c := NewCombat()
	c.SetSelfID(100)
	t0 := time.Unix(1000, 0)
	c.AddHit(t0, Hit{Actor: 100, Target: 200, Damage: 100, HitType: 1})
	c.AddHit(t0.Add(20*time.Second), Hit{Actor: 100, Target: 300, Damage: 50, HitType: 1})

	snap := c.Snapshot(t0.Add(20 * time.Second))
	if len(snap.Members) != 1 || snap.Members[0].Damage != 50 {
		t.Fatalf("members = %+v, want one with 50 (new target starts a new encounter)", snap.Members)
	}
}

// TestEncounterPhaseGraceExpires verifica que el mismo objetivo tras más de
// phaseGrace abre un encuentro nuevo.
func TestEncounterPhaseGraceExpires(t *testing.T) {
	c := NewCombat()
	c.SetSelfID(100)
	t0 := time.Unix(1000, 0)
	c.AddHit(t0, Hit{Actor: 100, Target: 200, Damage: 100, HitType: 1})
	c.AddHit(t0.Add(31*time.Second), Hit{Actor: 100, Target: 200, Damage: 50, HitType: 1})

	snap := c.Snapshot(t0.Add(31 * time.Second))
	if len(snap.Members) != 1 || snap.Members[0].Damage != 50 {
		t.Fatalf("members = %+v, want one with 50 (grace expired)", snap.Members)
	}
}

// TestBossEncounterLongGrace verifica que un jefe mantiene el encuentro durante
// una transición de fase larga y que el snapshot expone su id y duración.
func TestBossEncounterLongGrace(t *testing.T) {
	c := NewCombat()
	c.SetSelfID(100)
	c.MarkBoss(200)
	t0 := time.Unix(1000, 0)
	c.AddHit(t0, Hit{Actor: 100, Target: 200, Damage: 100, HitType: 1})
	c.AddHit(t0.Add(45*time.Second), Hit{Actor: 100, Target: 200, Damage: 50, HitType: 1})

	snap := c.Snapshot(t0.Add(45 * time.Second))
	if len(snap.Members) != 1 || snap.Members[0].Damage != 150 {
		t.Fatalf("members = %+v, want one with 150", snap.Members)
	}
	if !snap.IsBoss || snap.BossID != 200 || snap.Boss != "Jefe #200" {
		t.Errorf("boss = %q id=%d isBoss=%v, want Jefe #200", snap.Boss, snap.BossID, snap.IsBoss)
	}
	if !snap.Active {
		t.Errorf("boss encounter should stay active during grace")
	}
}

// TestBossEncounterContinuesOnAdds verifica que un add durante una fase no
// parte el encuentro del jefe.
func TestBossEncounterContinuesOnAdds(t *testing.T) {
	c := NewCombat()
	c.SetSelfID(100)
	c.MarkBoss(200)
	t0 := time.Unix(1000, 0)
	c.AddHit(t0, Hit{Actor: 100, Target: 200, Damage: 100, HitType: 1})
	c.AddHit(t0.Add(20*time.Second), Hit{Actor: 100, Target: 300, Damage: 50, HitType: 1})

	snap := c.Snapshot(t0.Add(20 * time.Second))
	if len(snap.Members) != 1 || snap.Members[0].Damage != 150 {
		t.Fatalf("members = %+v, want one with 150", snap.Members)
	}
	if !snap.IsBoss || snap.BossID != 200 {
		t.Errorf("boss = %q id=%d, want Jefe #200", snap.Boss, snap.BossID)
	}
}

// TestBossRemovalClosesEncounter verifica que la retirada del jefe congela el
// encuentro y que el siguiente golpe abre uno nuevo.
func TestBossRemovalClosesEncounter(t *testing.T) {
	c := NewCombat()
	c.SetSelfID(100)
	c.MarkBoss(200)
	t0 := time.Unix(1000, 0)
	c.AddHit(t0, Hit{Actor: 100, Target: 200, Damage: 100, HitType: 1})
	c.MarkRemoved(200)

	snap := c.Snapshot(t0.Add(time.Second))
	if snap.Active {
		t.Errorf("encounter should be closed after boss removal")
	}
	if len(snap.Members) != 1 || snap.Members[0].Damage != 100 {
		t.Fatalf("members = %+v, want frozen 100", snap.Members)
	}

	c.AddHit(t0.Add(2*time.Second), Hit{Actor: 100, Target: 200, Damage: 50, HitType: 1})
	snap = c.Snapshot(t0.Add(2 * time.Second))
	if len(snap.Members) != 1 || snap.Members[0].Damage != 50 {
		t.Fatalf("members = %+v, want new encounter with 50", snap.Members)
	}
}

// TestBossDifferentBossSplits verifica que dos jefes distintos no se fusionan
// aunque el segundo llegue enseguida.
func TestBossDifferentBossSplits(t *testing.T) {
	c := NewCombat()
	c.SetSelfID(100)
	c.MarkBoss(200)
	c.MarkBoss(300)
	t0 := time.Unix(1000, 0)
	c.AddHit(t0, Hit{Actor: 100, Target: 200, Damage: 100, HitType: 1})
	c.AddHit(t0.Add(time.Second), Hit{Actor: 100, Target: 300, Damage: 50, HitType: 1})

	snap := c.Snapshot(t0.Add(time.Second))
	if len(snap.Members) != 1 || snap.Members[0].Damage != 50 {
		t.Fatalf("members = %+v, want new encounter with 50 for the second boss", snap.Members)
	}
	if snap.BossID != 300 {
		t.Errorf("bossId = %d, want 300", snap.BossID)
	}
}

// TestNonBossSnapshotHasNoBoss verifica que los encuentros normales no exponen
// información de jefe.
func TestNonBossSnapshotHasNoBoss(t *testing.T) {
	c := NewCombat()
	c.SetSelfID(100)
	t0 := time.Unix(1000, 0)
	c.AddHit(t0, Hit{Actor: 100, Target: 200, Damage: 100, HitType: 1})
	snap := c.Snapshot(t0)
	if snap.IsBoss || snap.Boss != "" || snap.BossID != 0 {
		t.Errorf("boss = %q id=%d isBoss=%v, want no boss", snap.Boss, snap.BossID, snap.IsBoss)
	}
}

// TestParserMarksBossSpawn verifica que la máscara 0x220c marca el NPC como
// jefe al parsear un spawn 41 36.
func TestParserMarksBossSpawn(t *testing.T) {
	c := NewCombat()
	p := NewParser(c)
	// 0c = longitud, 41 36 = opcode, c8 01 = id 200, 0c 22 00 37 = 0x3700220c.
	p.ConsumeStream([]byte{0x0c, 0x41, 0x36, 0xc8, 0x01, 0x0c, 0x22, 0x00, 0x37}, time.Unix(1000, 0))
	e := findEntity(t, c, 200)
	if !e.Boss || !e.NPC {
		t.Fatalf("entity = %+v, want NPC boss", e)
	}
}

// TestParserLeavesNormalNPCNotBoss verifica que una máscara normal no marca el
// NPC como jefe.
func TestParserLeavesNormalNPCNotBoss(t *testing.T) {
	c := NewCombat()
	p := NewParser(c)
	// 0c 20 00 07 = 0x0700200c, un NPC normal.
	p.ConsumeStream([]byte{0x0c, 0x41, 0x36, 0xc8, 0x01, 0x0c, 0x20, 0x00, 0x07}, time.Unix(1000, 0))
	e := findEntity(t, c, 200)
	if e.Boss {
		t.Fatalf("entity = %+v, want non-boss", e)
	}
}

// TestParserClosesBossOnRemove verifica que 42 36 <id> 00 <flag> cierra el
// encuentro del jefe.
func TestParserClosesBossOnRemove(t *testing.T) {
	c := NewCombat()
	c.SetSelfID(100)
	c.MarkBoss(200)
	t0 := time.Unix(1000, 0)
	c.AddHit(t0, Hit{Actor: 100, Target: 200, Damage: 100, HitType: 1})

	p := NewParser(c)
	// 0a = longitud, 42 36 = opcode, c8 01 = id 200, 00 03 = payload.
	p.ConsumeStream([]byte{0x0a, 0x42, 0x36, 0xc8, 0x01, 0x00, 0x03}, t0.Add(time.Second))

	snap := c.Snapshot(t0.Add(time.Second))
	if snap.Active {
		t.Errorf("encounter should be closed after 42 36 removal")
	}
}

// TestNonGroupActorIgnored verifica que el daño de un actor fuera del grupo se
// descarta una vez conocida la identidad propia.
func TestNonGroupActorIgnored(t *testing.T) {
	c := NewCombat()
	c.SetSelfID(100)
	t0 := time.Unix(1000, 0)
	c.AddHit(t0, Hit{Actor: 999, Target: 200, Damage: 500, HitType: 1})

	if snap := c.Snapshot(t0); len(snap.Members) != 0 {
		t.Fatalf("members = %+v, want none (actor 999 is not in the group)", snap.Members)
	}
}

// TestPartyMemberCounted verifica que un miembro del grupo con identidad
// resuelta se cuenta y aparece en la tabla.
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

// TestPartyResolvesNameBeforeIdentity cubre el orden inverso: la identidad llega
// antes que el roster y el roster debe enlazar igualmente el nombre.
func TestPartyResolvesNameBeforeIdentity(t *testing.T) {
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

// TestClearParty verifica que al salir del grupo se olvida el roster y deja de
// contarse el daño de sus miembros.
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

// TestUnknownSelfIsPermissive verifica el modo permisivo: mientras no se conoce
// el id propio se muestra todo, y al conocerse se descarta lo anterior.
func TestUnknownSelfIsPermissive(t *testing.T) {
	c := NewCombat()
	t0 := time.Unix(1000, 0)
	c.AddHit(t0, Hit{Actor: 999, Target: 200, Damage: 500, HitType: 1})
	if snap := c.Snapshot(t0); len(snap.Members) != 1 {
		t.Fatalf("members = %+v, want one while self is unknown", snap.Members)
	}
	// Al conocerse self, se descartan los datos previos a la identidad.
	c.SetSelfID(100)
	if snap := c.Snapshot(t0); len(snap.Members) != 0 {
		t.Fatalf("members = %+v, want none after self is resolved", snap.Members)
	}
}

// TestSummonMergedIntoOwner verifica que un summon/hechizo de suelo que lleva el
// nombre del dueño suma su daño en la fila única del dueño.
func TestSummonMergedIntoOwner(t *testing.T) {
	c := NewCombat()
	c.SetSelfID(100)
	c.SetParty([]rosterMember{{Name: "Hero", Class: 25, Level: 33}})
	c.SetIdentity(100, "Hero", true, 0, 25, 33)
	c.SetName(200, "Hero") // summon con el nombre de su dueño
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

// TestMultiplePlayerIDsMerged verifica que un mismo jugador con varios ids de
// entidad (recarga de zona) ocupa una sola fila.
func TestMultiplePlayerIDsMerged(t *testing.T) {
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

// TestSummonAttributedByOwner verifica que un summon sin nombre propio se
// atribuye a su dueño a través del id del dueño.
func TestSummonAttributedByOwner(t *testing.T) {
	c := NewCombat()
	c.SetSelfID(100)
	c.SetParty([]rosterMember{{Name: "Ally", Class: 11, Level: 40}})
	c.SetIdentity(300, "Ally", false, 0, 11, 40)
	c.MarkNPC(500, 300) // summon de Ally, sin nombre propio

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

// TestForeignSummonIgnored verifica que una mascota de un jugador fuera del grupo
// no se cuenta.
func TestForeignSummonIgnored(t *testing.T) {
	c := NewCombat()
	c.SetSelfID(100)
	c.MarkNPC(500, 999) // el dueño 999 no está en el grupo

	t0 := time.Unix(1000, 0)
	c.AddHit(t0, Hit{Actor: 500, Target: 900, Damage: 40, HitType: 1})
	if snap := c.Snapshot(t0); len(snap.Members) != 0 {
		t.Fatalf("members = %+v, want none for a foreign pet", snap.Members)
	}
}
