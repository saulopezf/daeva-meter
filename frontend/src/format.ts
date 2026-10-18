// fmtCompact formatea un número grande de forma abreviada para la interfaz:
// 1.23M, 456k, 12.3k, etc. Los umbrales eligen la unidad para que el resultado
// ocupe pocos caracteres sin perder demasiada precisión.
export function fmtCompact(n: number): string {
  if (n >= 1_000_000) return (n / 1_000_000).toFixed(2) + 'M';
  if (n >= 100_000) return Math.round(n / 1000) + 'k';
  if (n >= 10_000) return (n / 1000).toFixed(1) + 'k';
  if (n >= 1000) return (n / 1000).toFixed(2) + 'k';
  return Math.round(n).toString();
}
