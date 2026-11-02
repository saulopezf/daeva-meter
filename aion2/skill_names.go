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
// obligatorios; Icon es opcional y el resto son metadatos reservados para
// funciones futuras (clase, tipo, descripción...).
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

// skillInfoFor busca una habilidad en el catálogo. El protocolo envía variantes
// de nivel/especialización en los últimos cuatro dígitos del id (por ejemplo
// 12240450 para "Judgment" 12240000), así que se prueba primero el id exacto y
// después su base. El catálogo es de solo lectura, de modo que esto se puede
// llamar desde cualquier goroutine sin bloquear.
func skillInfoFor(id int) (SkillInfo, bool) {
	if skill, ok := skillByID[id]; ok {
		return skill, true
	}
	if base := id / 10000 * 10000; base != id {
		if skill, ok := skillByID[base]; ok {
			return skill, true
		}
	}
	return SkillInfo{}, false
}

// SkillName devuelve el nombre visible de una habilidad o "" si aún no se
// conoce. La interfaz usa el id como fallback.
func SkillName(id int) string {
	skill, _ := skillInfoFor(id)
	return skill.Name
}

// SkillIcon devuelve la ruta del icono de una habilidad, relativa a la raíz del
// frontend (por ejemplo "skills/icons/ICON_TE_SKILL_004.png"), o "" si no hay
// icono. La interfaz la sirve como /<ruta>.
func SkillIcon(id int) string {
	skill, _ := skillInfoFor(id)
	return skill.Icon
}

// SkillByID devuelve la entrada completa del catálogo para un id exacto,
// incluidos los metadatos opcionales, y si existe. Para las variantes de nivel
// usa SkillName/SkillIcon, que caen a la base.
func SkillByID(id int) (SkillInfo, bool) {
	skill, ok := skillByID[id]
	return skill, ok
}
