package aion2

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"strings"
)

// skillCatalogJSON es el catálogo de habilidades embebido en el ejecutable.
// El archivo se edita en el repositorio; para que los cambios lleguen al .exe
// hay que recompilar.
//
//go:embed skill_catalog.json
var skillCatalogJSON []byte

// SkillInfo es una entrada del catálogo de habilidades. Solo ID y Name son
// obligatorios; el resto son metadatos opcionales reservados para funciones
// futuras (iconos, clase, tipo, descripción...).
type SkillInfo struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Icon        string `json:"icon,omitempty"`
	Class       string `json:"class,omitempty"`
	Type        string `json:"type,omitempty"`
	Description string `json:"description,omitempty"`
}

// skillCatalog es la forma del archivo JSON. Version queda reservado para
// futuras migraciones del formato.
type skillCatalog struct {
	Version int         `json:"version"`
	Skills  []SkillInfo `json:"skills"`
}

// skillByID se construye una sola vez al arrancar y después es de solo lectura,
// así que se puede consultar desde cualquier goroutine sin bloqueo.
var skillByID = loadEmbeddedSkillCatalog()

func loadEmbeddedSkillCatalog() map[int]SkillInfo {
	catalog, errs := parseSkillCatalog(skillCatalogJSON)
	for _, err := range errs {
		log.Printf("[skills] %v", err)
	}
	return catalog
}

// parseSkillCatalog parsea y valida el catálogo. Devuelve las entradas válidas
// y los errores encontrados: una entrada inválida no impide usar las demás.
func parseSkillCatalog(data []byte) (map[int]SkillInfo, []error) {
	// Algunos editores de Windows guardan UTF-8 con BOM. encoding/json no lo
	// acepta, así que lo quitamos antes de parsear.
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})

	var doc skillCatalog
	if err := json.Unmarshal(data, &doc); err != nil {
		return map[int]SkillInfo{}, []error{fmt.Errorf("decode skill catalog: %w", err)}
	}

	byID := make(map[int]SkillInfo, len(doc.Skills))
	var errs []error
	for i, skill := range doc.Skills {
		skill.Name = strings.TrimSpace(skill.Name)
		if skill.ID <= 0 {
			errs = append(errs, fmt.Errorf("skills[%d]: invalid id %d", i, skill.ID))
			continue
		}
		if skill.Name == "" {
			errs = append(errs, fmt.Errorf("skills[%d] (id %d): empty name", i, skill.ID))
			continue
		}
		if prev, ok := byID[skill.ID]; ok {
			errs = append(errs, fmt.Errorf("skills[%d]: duplicate id %d (%q overrides %q)",
				i, skill.ID, skill.Name, prev.Name))
		}
		byID[skill.ID] = skill
	}
	return byID, errs
}

// SkillName devuelve el nombre visible de una habilidad o "" si aún no se
// conoce. La interfaz usa el id como fallback.
func SkillName(id int) string {
	return skillByID[id].Name
}

// SkillByID devuelve la entrada completa del catálogo, incluidos los metadatos
// opcionales, y si existe.
func SkillByID(id int) (SkillInfo, bool) {
	skill, ok := skillByID[id]
	return skill, ok
}
