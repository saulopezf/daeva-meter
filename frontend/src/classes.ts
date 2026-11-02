// Emblemas de clase de Aion 2. Los archivos viven en frontend/public/classes/
//
// Los nombres internos del arte no siempre coinciden con los del roster:
// "Elementalist" usa el emblema "elementalist" (el antiguo Spiritmaster) y
// "Brawler" el de "fighter".
const EMBLEMS: Record<string, string> = {
  Gladiator: "gladiator",
  Templar: "templar",
  Ranger: "ranger",
  Assassin: "assassin",
  Elementalist: "elementalist",
  Sorcerer: "sorcerer",
  Cleric: "cleric",
  Chanter: "chanter",
  Brawler: "fighter",
};

// classEmblem devuelve la URL del emblema de una clase, o "" si no se conoce.
export function classEmblem(className: string): string {
  const file = EMBLEMS[className];
  return file ? `/classes/${file}.webp` : "";
}
