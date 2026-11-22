import { useCallback, useEffect, useState } from 'react';

const STORAGE_KEY = 'daeva.opacity';
const DEFAULT_OPACITY = 0.75;
export const MIN_OPACITY = 0.2;

function clamp(value: number): number {
  if (!Number.isFinite(value)) return DEFAULT_OPACITY;
  return Math.min(1, Math.max(MIN_OPACITY, value));
}

function readStored(): number {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    return raw === null ? DEFAULT_OPACITY : clamp(Number(raw));
  } catch {
    return DEFAULT_OPACITY;
  }
}

function apply(value: number) {
  document.documentElement.style.setProperty('--alpha', String(value));
}

// useOpacity controla la opacidad del fondo de las ventanas. El valor se guarda en
// localStorage y se aplica como la variable CSS --alpha, que solo afecta a los
// fondos (el texto sigue opaco). Todas las ventanas comparten origen, así que el
// evento "storage" sincroniza la ventana de detalle con la principal.
export function useOpacity(): [number, (value: number) => void] {
  const [opacity, setOpacityState] = useState(readStored);

  useEffect(() => {
    apply(opacity);
  }, [opacity]);

  useEffect(() => {
    const onStorage = (e: StorageEvent) => {
      if (e.key === STORAGE_KEY) setOpacityState(readStored());
    };
    window.addEventListener('storage', onStorage);
    return () => window.removeEventListener('storage', onStorage);
  }, []);

  const setOpacity = useCallback((value: number) => {
    const next = clamp(value);
    setOpacityState(next);
    try {
      localStorage.setItem(STORAGE_KEY, String(next));
    } catch {
      // Sin almacenamiento disponible: el valor solo dura esta sesión.
    }
  }, []);

  return [opacity, setOpacity];
}
