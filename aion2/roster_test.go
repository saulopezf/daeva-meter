package aion2

import (
	"testing"
	"time"
)

func le32bytes(v uint32) []byte {
	return []byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)}
}

func le64bytes(v uint64) []byte {
	b := make([]byte, 8)
	for i := 0; i < 8; i++ {
		b[i] = byte(v >> (8 * i))
	}
	return b
}

// buildRoster encodes a synthetic 02 97 record following the §8.6 layout,
// including a variable-length tail before each next member.
func buildRoster(partyName string, members []rosterMember) []byte {
	b := []byte{0x02, 0x97}
	b = append(b, le32bytes(0x1234)...) // party_key
	b = append(b, byte(len(partyName)))
	b = append(b, partyName...)
	b = append(b, byte(len(members)))               // party_size
	b = append(b, le32bytes(0x99)...)               // dungeon_id
	b = append(b, 0x00, 0x00)                       // 2 bytes
	b = append(b, le64bytes(0x0006000000000001)...) // leader_dbid
	b = append(b, 0x00, 0x00, 0x00)                 // 3 bytes
	b = append(b, encodeVarint(len(members))...)
	for i, m := range members {
		b = append(b, 0x01)      // mask
		b = append(b, byte(i+1)) // slot
		dbid := uint64(2000)<<48 | uint64(i+1)
		b = append(b, le64bytes(dbid)...)
		b = append(b, byte(len(m.Name)))
		b = append(b, m.Name...)
		b = append(b, le32bytes(uint32(m.Class))...)
		b = append(b, le32bytes(uint32(m.Level))...)
		b = append(b, le32bytes(0)...) // gear_score
		if i != len(members)-1 {
			b = append(b, 0x07, 0x02, 0x06, 0x00, 0x00, 0x00, 0x00) // variable tail
		}
	}
	return b
}

func TestParseRoster(t *testing.T) {
	want := []rosterMember{
		{Name: "Aelira", Class: 25, Level: 33},
		{Name: "Buddy", Class: 11, Level: 40},
	}
	buf := buildRoster("Party", want)
	got, ok := parseRosterAt(buf, 0)
	if !ok {
		t.Fatal("parseRosterAt failed on a valid roster")
	}
	if len(got) != len(want) {
		t.Fatalf("members = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("member %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestParseRosterSingleMember(t *testing.T) {
	want := []rosterMember{{Name: "Aelira", Class: 25, Level: 33}}
	got, ok := parseRosterAt(buildRoster("Solo", want), 0)
	if !ok || len(got) != 1 || got[0] != want[0] {
		t.Fatalf("got %+v ok=%v, want %+v", got, ok, want)
	}
}

func TestParseRosterRejectsGarbage(t *testing.T) {
	// A stray 02 97 pair inside unrelated bytes must not parse.
	garbage := []byte{0x02, 0x97, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}
	if _, ok := parseRosterAt(garbage, 0); ok {
		t.Fatal("parseRosterAt accepted garbage")
	}
}

func TestEmbeddedRosterInPacket(t *testing.T) {
	// A roster that travels inside an LZ4 bundle embedded in another packet
	// must still be found (spec §6.2).
	roster := buildRoster("Party", []rosterMember{{Name: "Buddy", Class: 11, Level: 40}})
	inner := buildBundleFrame(t, roster)
	payload := append([]byte{0x06, 0x38, 0x01}, inner...)
	outer := append(encodeVarint(len(payload)+4), payload...)

	c := NewCombat()
	p := NewParser(c)
	if got := p.ConsumeStream(outer, time.Now()); got != len(outer) {
		t.Fatalf("consumed=%d, want %d", got, len(outer))
	}
	snap := c.Snapshot(time.Now())
	if len(snap.Party) != 1 || snap.Party[0] != "Buddy" {
		t.Fatalf("party = %v, want [Buddy]", snap.Party)
	}
}
