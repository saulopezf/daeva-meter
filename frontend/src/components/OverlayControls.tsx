import { useEffect, useRef, useState } from 'react';
import { fmtClock } from '../format';
import type { EncounterSummary } from '../types';
import { MIN_OPACITY } from '../useOpacity';

interface OverlayControlsProps {
  history: EncounterSummary[];
  selectedID: number;
  onSelect: (id: number) => void;
  opacity: number;
  onOpacity: (value: number) => void;
  clickThrough: boolean;
  onClickThrough: (on: boolean) => void;
}

function historyLabel(entry: EncounterSummary): string {
  const kind = entry.isBoss ? entry.boss || 'Boss' : 'Fight';
  const time =
    entry.startedAt > 0
      ? new Date(entry.startedAt).toLocaleTimeString([], {
          hour: '2-digit',
          minute: '2-digit',
          hour12: false,
        })
      : '';
  return `${kind} · ${fmtClock(entry.elapsed)}${time ? ` · ${time}` : ''}`;
}

// OverlayControls agrupa los iconos de la barra de título del overlay:
// historial de combates, opacidad del fondo y modo click-through.
export function OverlayControls({
  history,
  selectedID,
  onSelect,
  opacity,
  onOpacity,
  clickThrough,
  onClickThrough,
}: OverlayControlsProps) {
  const [showOpacity, setShowOpacity] = useState(false);
  const popRef = useRef<HTMLDivElement>(null);

  // Cierra el popover de opacidad al pulsar fuera de él.
  useEffect(() => {
    if (!showOpacity) return;
    const onDown = (e: MouseEvent) => {
      if (popRef.current && !popRef.current.contains(e.target as Node)) {
        setShowOpacity(false);
      }
    };
    window.addEventListener('mousedown', onDown);
    return () => window.removeEventListener('mousedown', onDown);
  }, [showOpacity]);

  const isHistory = selectedID !== 0;

  return (
    <>
      <label
        className={`icon-btn history-btn${isHistory ? ' icon-btn-on' : ''}`}
        title={isHistory ? 'Viewing a past fight — choose Live to return' : 'Past fights'}
      >
        <svg viewBox="0 0 16 16" aria-hidden="true">
          <circle cx="8" cy="8" r="5.5" />
          <path d="M8 5v3l2 1.5" />
        </svg>
        <select
          className="history-select"
          value={String(selectedID)}
          onChange={(e) => onSelect(Number(e.target.value))}
          aria-label="Fight history"
        >
          <option value="0">Live</option>
          {history.map((entry) => (
            <option key={entry.id} value={String(entry.id)}>
              {historyLabel(entry)}
            </option>
          ))}
        </select>
      </label>

      <div className="opacity-wrap" ref={popRef}>
        <button
          type="button"
          className={`icon-btn${showOpacity ? ' icon-btn-on' : ''}`}
          onClick={() => setShowOpacity((v) => !v)}
          aria-label="Background opacity"
          aria-expanded={showOpacity}
          title="Background opacity"
        >
          <svg viewBox="0 0 16 16" aria-hidden="true">
            <circle cx="8" cy="8" r="5.5" />
            <path d="M8 2.5v11a5.5 5.5 0 0 0 0-11z" className="fill" />
          </svg>
        </button>
        {showOpacity && (
          <div className="opacity-pop">
            <input
              type="range"
              min={MIN_OPACITY * 100}
              max={100}
              step={5}
              value={Math.round(opacity * 100)}
              onChange={(e) => onOpacity(Number(e.target.value) / 100)}
              aria-label="Background opacity"
            />
            <span className="opacity-value">{Math.round(opacity * 100)}%</span>
          </div>
        )}
      </div>

      <button
        type="button"
        className={`icon-btn${clickThrough ? ' icon-btn-on' : ''}`}
        onClick={() => onClickThrough(!clickThrough)}
        aria-label="Click-through mode"
        aria-pressed={clickThrough}
        title="Click-through: clicks pass to the game (Ctrl+Alt+M to toggle)"
      >
        <svg viewBox="0 0 16 16" aria-hidden="true">
          <path d="M4 2.5l8.5 5-3.8.9 2.2 4.1-1.6.8-2.2-4.1L4 11.8z" />
        </svg>
      </button>
    </>
  );
}
