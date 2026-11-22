import type { ReactNode } from 'react';
import { Window } from '@wailsio/runtime';

interface TitleBarProps {
  label?: string;
  // Contenido central (p. ej. la información del combate). Si no se indica se
  // muestra la marca de la app.
  children?: ReactNode;
  // Botones que se colocan antes de la cruz de cerrar.
  actions?: ReactNode;
}

// TitleBar sustituye a la barra de título nativa de Windows. Toda la barra
// (excepto los botones) sirve para arrastrar la ventana frameless.
export function TitleBar({ label = 'daeva-meter', children, actions }: TitleBarProps) {
  return (
    <div className="titlebar">
      <div className="titlebar-main">
        {children ?? (
          <span className="titlebar-brand">
            <i className="brand-dot" />
            {label}
          </span>
        )}
      </div>
      <div className="titlebar-actions">
        {actions}
        <button
          type="button"
          className="icon-btn titlebar-close"
          onClick={() => {
            void Window.Close();
          }}
          aria-label="Close window"
          title="Close"
        >
          <svg viewBox="0 0 12 12" aria-hidden="true">
            <path d="M2.5 2.5l7 7M9.5 2.5l-7 7" />
          </svg>
        </button>
      </div>
    </div>
  );
}
