import { Window } from '@wailsio/runtime';

interface TitleBarProps {
  label?: string;
}

// TitleBar sustituye a la barra de título nativa de Windows: se integra con el
// fondo de la app y solo muestra una cruz para cerrar. Toda la barra (excepto la
// cruz) sirve para arrastrar la ventana frameless.
export function TitleBar({ label = 'daeva-meter' }: TitleBarProps) {
  return (
    <div className="titlebar">
      <span className="titlebar-brand">
        <i className="brand-dot" />
        {label}
      </span>
      <button
        type="button"
        className="titlebar-close"
        onClick={() => {
          void Window.Close();
        }}
        aria-label="Close window"
        title="Close"
      >
        <svg width="11" height="11" viewBox="0 0 11 11" aria-hidden="true">
          <path d="M1 1l9 9M10 1L1 10" />
        </svg>
      </button>
    </div>
  );
}
