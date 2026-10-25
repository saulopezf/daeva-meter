package aion2

import "testing"

// TestSkillName comprueba el fallback y la consulta del catálogo en memoria.
func TestSkillName(t *testing.T) {
	const id = 987654321
	if got := SkillName(id); got != "" {
		t.Fatalf("SkillName(%d) = %q, want empty fallback", id, got)
	}

	skillByID[id] = SkillInfo{ID: id, Name: "Test Skill"}
	t.Cleanup(func() { delete(skillByID, id) })

	if got := SkillName(id); got != "Test Skill" {
		t.Fatalf("SkillName(%d) = %q, want Test Skill", id, got)
	}
	if info, ok := SkillByID(id); !ok || info.Name != "Test Skill" {
		t.Fatalf("SkillByID(%d) = %+v, %v, want Test Skill", id, info, ok)
	}
}

// TestParseSkillCatalog comprueba la carga de nombres y metadatos opcionales.
func TestParseSkillCatalog(t *testing.T) {
	data := []byte(`{
		"version": 1,
		"skills": [
			{
				"id": 1,
				"name": " One ",
				"icon": "icons/1.png",
				"class": "Templar",
				"type": "active",
				"description": "desc"
			},
			{"id": 2, "name": "Two"}
		]
	}`)

	got, errs := parseSkillCatalog(data)
	if len(errs) != 0 {
		t.Fatalf("parseSkillCatalog errors = %v, want none", errs)
	}
	if len(got) != 2 {
		t.Fatalf("catalog = %+v, want two entries", got)
	}
	first := got[1]
	if first.Name != "One" || first.Icon != "icons/1.png" ||
		first.Class != "Templar" || first.Type != "active" ||
		first.Description != "desc" {
		t.Errorf("first entry = %+v, want trimmed name and metadata", first)
	}
	if got[2].Name != "Two" {
		t.Errorf("second entry = %+v, want Two", got[2])
	}
}

// TestParseSkillCatalogErrors comprueba que las entradas inválidas se ignoran
// sin perder las válidas. Un id duplicado se queda con la última entrada.
func TestParseSkillCatalogErrors(t *testing.T) {
	data := []byte(`{
		"version": 1,
		"skills": [
			{"id": 1, "name": "One"},
			{"id": 1, "name": "Uno"},
			{"id": 0, "name": "Bad"},
			{"id": 2, "name": "   "}
		]
	}`)

	got, errs := parseSkillCatalog(data)
	if len(errs) != 3 {
		t.Fatalf("errors = %v, want three (duplicate, invalid id, empty name)", errs)
	}
	if got[1].Name != "Uno" {
		t.Errorf("catalog[1] = %+v, want last entry Uno", got[1])
	}
	if _, ok := got[0]; ok {
		t.Errorf("catalog[0] exists, want invalid id skipped")
	}
	if _, ok := got[2]; ok {
		t.Errorf("catalog[2] exists, want empty name skipped")
	}
}

// TestParseSkillCatalogBOM acepta el BOM que añaden algunos editores de
// Windows al guardar UTF-8.
func TestParseSkillCatalogBOM(t *testing.T) {
	data := append([]byte{0xEF, 0xBB, 0xBF},
		[]byte(`{"version":1,"skills":[{"id":1,"name":"One"}]}`)...)

	got, errs := parseSkillCatalog(data)
	if len(errs) != 0 {
		t.Fatalf("parseSkillCatalog errors = %v, want none", errs)
	}
	if got[1].Name != "One" {
		t.Fatalf("catalog = %+v, want One", got)
	}
}

// TestParseSkillCatalogInvalidJSON comprueba el fallback ante un JSON roto.
func TestParseSkillCatalogInvalidJSON(t *testing.T) {
	got, errs := parseSkillCatalog([]byte(`{"skills":`))
	if len(errs) == 0 {
		t.Fatal("parseSkillCatalog accepted invalid JSON")
	}
	if len(got) != 0 {
		t.Fatalf("catalog = %+v, want empty", got)
	}
}

// TestEmbeddedSkillCatalogValid evita que un error de edición del JSON llegue
// al ejecutable sin que los tests lo detecten.
func TestEmbeddedSkillCatalogValid(t *testing.T) {
	if _, errs := parseSkillCatalog(skillCatalogJSON); len(errs) != 0 {
		t.Fatalf("embedded skill catalog = %v, want valid", errs)
	}
}
