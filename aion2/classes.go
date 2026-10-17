package aion2

// ClassName maps a roster class id to its display name and bar colour. Each
// class occupies a 4-wide id range (spec §6.1):
//
//	5-8 Gladiator, 9-12 Templar, 13-16 Ranger, 17-20 Assassin,
//	21-24 Elementalist, 25-28 Sorcerer, 29-32 Cleric, 33-36 Chanter.
func ClassName(id int) (string, string) {
	switch {
	case id >= 5 && id <= 8:
		return "Gladiator", "#fb923c"
	case id >= 9 && id <= 12:
		return "Templar", "#f87171"
	case id >= 13 && id <= 16:
		return "Ranger", "#38bdf8"
	case id >= 17 && id <= 20:
		return "Assassin", "#c084fc"
	case id >= 21 && id <= 24:
		return "Elementalist", "#a78bfa"
	case id >= 25 && id <= 28:
		return "Sorcerer", "#e879f9"
	case id >= 29 && id <= 32:
		return "Cleric", "#4ade80"
	case id >= 33 && id <= 36:
		return "Chanter", "#facc15"
	}
	return "", ""
}
