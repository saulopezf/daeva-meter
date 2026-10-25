package aion2

import (
	"testing"
	"time"
)

// TestMemberDetailSkillBreakdown comprueba la agregación por habilidad, los
// golpes extra y la cuota de daño de cada habilidad.
func TestMemberDetailSkillBreakdown(t *testing.T) {
	c := NewCombat()
	c.SetSelfID(100)
	c.SetIdentity(100, "Hero", true, 0, 21, 33)

	t0 := time.Unix(1000, 0)
	c.AddHit(t0, Hit{
		Actor: 100, Target: 900, Skill: 111,
		Damage: 100, ExtraHits: 2, ExtraDamage: 50, Crit: true,
	})
	c.AddHit(t0.Add(time.Second), Hit{
		Actor: 100, Target: 901, Skill: 111, Damage: 200,
	})
	c.AddHit(t0.Add(time.Second), Hit{
		Actor: 100, Target: 900, Skill: 222, Damage: 75,
	})

	now := t0.Add(time.Second)
	snap := c.Snapshot(now)
	if len(snap.Members) != 1 {
		t.Fatalf("members = %+v, want one", snap.Members)
	}
	if key := snap.Members[0].Key; key != "n:Hero" {
		t.Fatalf("key = %q, want n:Hero", key)
	}

	md := c.MemberDetail(now, snap.Members[0].Key)
	if !md.Found || md.Name != "Hero" || md.Class != "Elementalist" {
		t.Fatalf("detail = %+v, want found Hero/Elementalist", md)
	}
	if md.Damage != 425 || md.DPS != 425 {
		t.Errorf("damage/dps = %d/%.0f, want 425/425", md.Damage, md.DPS)
	}
	if md.Hits != 3 || md.ExtraHits != 2 || md.TotalHits != 5 || md.Crits != 1 {
		t.Errorf("hits/extra/total/crits = %d/%d/%d/%d, want 3/2/5/1",
			md.Hits, md.ExtraHits, md.TotalHits, md.Crits)
	}
	if len(md.Skills) != 2 {
		t.Fatalf("skills = %+v, want two", md.Skills)
	}

	first := md.Skills[0]
	if first.ID != 111 || first.Damage != 350 || first.Hits != 2 ||
		first.ExtraHits != 2 || first.TotalHits != 4 || first.Crits != 1 {
		t.Errorf("first skill = %+v, want id=111 damage=350 hits=2 extra=2 total=4 crits=1", first)
	}
	if first.AvgHit != 87.5 {
		t.Errorf("avg hit = %.1f, want 87.5", first.AvgHit)
	}
	if first.Share < 82.3 || first.Share > 82.4 {
		t.Errorf("share = %.2f, want ~82.35", first.Share)
	}
	if md.Skills[1].ID != 222 || md.Skills[1].Damage != 75 {
		t.Errorf("second skill = %+v, want id=222 damage=75", md.Skills[1])
	}
}

// TestMemberDetailMergesSummonSkills verifica que las habilidades de un summon
// se suman a la fila canónica de su dueño.
func TestMemberDetailMergesSummonSkills(t *testing.T) {
	c := NewCombat()
	c.SetSelfID(100)
	c.SetParty([]rosterMember{{Name: "Hero", Class: 21, Level: 33}})
	c.SetIdentity(100, "Hero", true, 0, 21, 33)
	c.SetName(200, "Hero")
	c.MarkNPC(200, 0)

	t0 := time.Unix(1000, 0)
	c.AddHit(t0, Hit{Actor: 100, Target: 900, Skill: 111, Damage: 100})
	c.AddHit(t0, Hit{Actor: 200, Target: 900, Skill: 222, Damage: 50})

	snap := c.Snapshot(t0)
	if len(snap.Members) != 1 {
		t.Fatalf("members = %+v, want one merged row", snap.Members)
	}
	md := c.MemberDetail(t0, snap.Members[0].Key)
	if md.Damage != 150 || len(md.Skills) != 2 {
		t.Fatalf("detail = %+v, want damage 150 and two skills", md)
	}
	if md.Skills[0].Damage != 100 || md.Skills[1].Damage != 50 {
		t.Errorf("skills = %+v, want 100 and 50", md.Skills)
	}
}

// TestMemberDetailNotFound cubre una clave inexistente y un combate vacío.
func TestMemberDetailNotFound(t *testing.T) {
	c := NewCombat()
	md := c.MemberDetail(time.Now(), "n:Nobody")
	if md.Found || md.Key != "n:Nobody" || len(md.Skills) != 0 {
		t.Fatalf("empty detail = %+v, want not found", md)
	}

	c.SetSelfID(100)
	t0 := time.Unix(1000, 0)
	c.AddHit(t0, Hit{Actor: 100, Target: 900, Skill: 111, Damage: 100})
	if md := c.MemberDetail(t0, "n:Nobody"); md.Found {
		t.Fatalf("detail = %+v, want not found", md)
	}
}

// TestBreakdownKeepsTargets comprueba que el detalle interno distingue el
// objetivo, base para el futuro desglose por enemigo.
func TestBreakdownKeepsTargets(t *testing.T) {
	c := NewCombat()
	c.SetSelfID(100)
	t0 := time.Unix(1000, 0)
	c.AddHit(t0, Hit{Actor: 100, Target: 900, Skill: 111, Damage: 100})
	c.AddHit(t0, Hit{Actor: 100, Target: 901, Skill: 111, Damage: 50})

	if got := len(c.cur.breakdown); got != 2 {
		t.Fatalf("breakdown entries = %d, want 2 (one per target)", got)
	}
}
