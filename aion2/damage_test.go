package aion2

import (
	"encoding/hex"
	"testing"
)

func encodeVarint(v int) []byte {
	var out []byte
	for {
		b := byte(v & 0x7F)
		v >>= 7
		if v != 0 {
			b |= 0x80
		}
		out = append(out, b)
		if v == 0 {
			break
		}
	}
	return out
}

// buildDamageFrame wraps the given hex (the bytes after the 04 38 opcode) in a
// frame with a correct leading varint length.
func buildDamageFrame(t *testing.T, hexBody string) []byte {
	t.Helper()
	raw, err := hex.DecodeString(hexBody)
	if err != nil {
		t.Fatalf("bad hex: %v", err)
	}
	body := append([]byte{0x04, 0x38}, raw...)
	L := len(body) + 4
	return append(encodeVarint(L), body...)
}

func TestDamageVectors(t *testing.T) {
	cases := []struct {
		name      string
		hexBody   string
		total     int
		extraHits int
		extraDmg  int
		target    int
		actor     int
		skill     int
		expectHit bool
	}{
		{"plain-under-126", "b1ea011600f30a40c0f40063028000010b199b5f01000000ac52d007", 976, 0, 0, 30001, 1395, 16040000, true},
		{"crit-two-extra", "b1ea013600f30a40c0f4007a038000010b199b5f01000000ac52a40d021818", 1700, 2, 48, 30001, 1395, 16040000, true},
		{"layout4-one-extra", "b1ea013400f30ae0b7f800cd028bd3276101000000ac52d330010159", 6227, 1, 89, 30001, 1395, 16300000, true},
		{"spirit-layout4", "fe9e0224009e9b01c3f8f5000302792b156002000000ac52e10301060200", 481, 1, 6, 0, 0, 0, true},
		{"plain-439", "fe9e0204009e9b01c3f8f5000302792b156003000000ac52b7030300", 439, 0, 0, 0, 0, 0, true},
		{"parry-front", "e9de020600c80b9147ff003f02020002aff4b76301000000e4506a0100", 106, 0, 0, 44905, 1480, 16730001, true},
		{"double", "bfe6020600d23310ffd6006f0208000055a2fb5302000000aa56d20e0200", 1874, 0, 0, 0, 0, 0, true},
		{"big-1976", "a8a3041400be77802a04015d02109aa06501000000dc51b80f010100", 1976, 0, 0, 70056, 15294, 17050240, true},
		{"resist", "a8a3040001be77802a04015d06139aa06501000000dc5101c9e1f5050100", 0, 0, 0, 0, 0, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := NewCombat()
			var got *Hit
			c.OnHit = func(h Hit) { hh := h; got = &hh }
			p := NewParser(c)
			frame := buildDamageFrame(t, tc.hexBody)
			p.parseDamage(frame, false, false)

			if !tc.expectHit {
				if got != nil {
					t.Fatalf("expected no hit, got %+v", *got)
				}
				return
			}
			if got == nil {
				t.Fatalf("expected a hit, got none")
			}
			if total := got.Damage + got.ExtraDamage; total != tc.total {
				t.Errorf("total damage = %d, want %d (Damage=%d Extra=%d)", total, tc.total, got.Damage, got.ExtraDamage)
			}
			if got.ExtraHits != tc.extraHits {
				t.Errorf("extra hits = %d, want %d", got.ExtraHits, tc.extraHits)
			}
			if got.ExtraDamage != tc.extraDmg {
				t.Errorf("extra damage = %d, want %d", got.ExtraDamage, tc.extraDmg)
			}
			if tc.target != 0 && got.Target != tc.target {
				t.Errorf("target = %d, want %d", got.Target, tc.target)
			}
			if tc.actor != 0 && got.Actor != tc.actor {
				t.Errorf("actor = %d, want %d", got.Actor, tc.actor)
			}
			if tc.skill != 0 && got.Skill != tc.skill {
				t.Errorf("skill = %d, want %d", got.Skill, tc.skill)
			}
		})
	}
}
