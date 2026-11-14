import { useEffect, useRef } from 'react';
import { Window } from '@wailsio/runtime';

// useDynamicWindowHeight ajusta la altura de la ventana al contenido real de la
// app. El elemento observado debe tener altura automática (clase "app-auto");
// así el contenedor crece con las barras de DPS y se encoge cuando desaparecen.
//
// La altura se limita al área visible de la pantalla: si el contenido es mayor,
// el contenedor se queda en ese máximo y las filas pasan a hacer scroll.
export function useDynamicWindowHeight<T extends HTMLElement>() {
  const ref = useRef<T>(null);

  useEffect(() => {
    const el = ref.current;
    if (!el) return;

    const available = window.screen.availHeight;
    const maxHeight = available > 0 ? Math.max(160, available - 48) : 2000;
    el.style.maxHeight = `${maxHeight}px`;

    let frame = 0;
    let cancelled = false;
    let lastSetHeight = -1;

    const apply = async () => {
      const height = Math.ceil(el.getBoundingClientRect().height);
      if (height <= 0 || height === lastSetHeight) return;
      try {
        const size = await Window.Size();
        if (cancelled) return;
        if (size.height !== height) {
          await Window.SetSize(size.width, height);
        }
        lastSetHeight = height;
      } catch {
        // En un navegador normal no existe el runtime de Wails; se ignora.
      }
    };

    const schedule = () => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(() => void apply());
    };

    const observer = new ResizeObserver(schedule);
    observer.observe(el);
    schedule();

    return () => {
      cancelled = true;
      observer.disconnect();
      cancelAnimationFrame(frame);
    };
  }, []);

  return ref;
}
