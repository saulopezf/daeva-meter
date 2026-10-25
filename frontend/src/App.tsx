import { useCallback, useEffect, useState } from 'react';
import { Events } from '@wailsio/runtime';
import { App as Backend } from '../bindings/daeva-meter';
import { DpsBar } from './components/DpsBar';
import { fmtCompact } from './format';
import type { CaptureState, CombatSnapshot, CombatMember, Stats } from './types';

// Valores por defecto mientras llega el primer estado desde el backend.
const EMPTY_STATS: Stats = {
  devices: 0,
  packets: 0,
  bytes: 0,
  payloadBytes: 0,
  streams: 0,
  gaps: 0,
  packetsPerSec: 0,
  bytesPerSec: 0,
  trafficSeen: false,
};

const EMPTY_COMBAT: CombatSnapshot = {
  active: false,
  elapsed: 0,
  selfId: 0,
  self: '',
  members: [],
  party: [],
};

// Estado global de la captura derivado del backend y de las estadísticas.
type Status = 'connecting' | 'live' | 'listening' | 'stopped' | 'error';

const STATUS_LABEL: Record<Status, string> = {
  connecting: 'CONNECTING',
  live: 'LIVE',
  listening: 'LISTENING',
  stopped: 'STOPPED',
  error: 'ERROR',
};

// App es la raíz de la interfaz. Consulta el estado inicial, se suscribe a los
// eventos del backend (estadísticas, logs y actualizaciones de combate) y pinta
// la cabecera, la tabla de DPS y el pie con contadores.
export default function App() {
  const [state, setState] = useState<CaptureState | null>(null);
  const [stats, setStats] = useState<Stats>(EMPTY_STATS);
  const [combat, setCombat] = useState<CombatSnapshot>(EMPTY_COMBAT);
  const [logs, setLogs] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);

  // pushLog conserva solo las últimas 5 líneas de log para el pie.
  const pushLog = useCallback((msg: string) => {
    setLogs((prev) => [...prev.slice(-4), msg]);
  }, []);

  // refresh trae el estado completo del backend (captura + combate) bajo demanda.
  const refresh = useCallback(async () => {
    try {
      const s = await Backend.CaptureStatus();
      setState(s);
      setStats(s.stats);
      setCombat(await Backend.CombatState());
    } catch (e) {
      pushLog(String(e));
    }
  }, [pushLog]);

  // Al montar: carga el estado y se suscribe a los eventos del backend. Los
  // eventos "combat:update" llegan dos veces por segundo, así que la tabla se
  // refresca en vivo sin polling.
  useEffect(() => {
    void refresh();
    const offStats = Events.On('capture:stats', (ev) => setStats(ev.data as Stats));
    const offLog = Events.On('capture:log', (ev) => pushLog(ev.data as string));
    const offCombat = Events.On('combat:update', (ev) => setCombat(ev.data as CombatSnapshot));
    return () => {
      offStats();
      offLog();
      offCombat();
    };
  }, [refresh, pushLog]);

  // toggle arranca o detiene la captura según el estado actual.
  const toggle = useCallback(async () => {
    setBusy(true);
    try {
      if (state?.running) {
        await Backend.StopCapture();
      } else {
        await Backend.StartCapture();
      }
    } catch (e) {
      pushLog(String(e));
    } finally {
      await refresh();
      setBusy(false);
    }
  }, [state, refresh, pushLog]);

  // Deriva la etiqueta de estado a partir de los datos del backend.
  const status: Status =
    state === null
      ? 'connecting'
      : state.error !== ''
        ? 'error'
        : !state.running
          ? 'stopped'
          : stats.trafficSeen
            ? 'live'
            : 'listening';

  const members: CombatMember[] = combat.members ?? [];
  // Máximo de DPS para escalar las barras de forma relativa al primero.
  const maxDps = Math.max(...members.map((m) => m.dps), 1);
  const lastLog = logs.length > 0 ? logs[logs.length - 1] : '';

  return (
    <div className="app">
      <header>
        <span className={`badge badge-${status}`}>
          <i className="dot" />
          {STATUS_LABEL[status]}
        </span>
        <span className="port">tcp/13328</span>
        {state?.logPath && (
          <span className="port" title="session log file">
            {state.logPath}
          </span>
        )}
        <button className="btn" onClick={() => void toggle()} disabled={busy || status === 'connecting'}>
          {state?.running ? 'Stop' : 'Start'}
        </button>
      </header>

      {state?.error && <div className="error">{state.error}</div>}

      <div className="section-label">
        <span>Party DPS</span>
        <span className={combat.active ? 'live-tag' : 'mock'}>
          {combat.active
            ? `FIGHT ${combat.elapsed.toFixed(0)}s`
            : members.length
              ? `ENDED ${combat.elapsed.toFixed(0)}s`
              : 'IDLE'}
        </span>
      </div>

      <main className="rows">
        {members.length === 0 ? (
          <div className="empty">waiting for combat data…</div>
        ) : (
          members.map((m, i) => (
            <DpsBar key={m.id} member={m} rank={i + 1} maxDps={maxDps} />
          ))
        )}
      </main>

      <footer>
        <div className="stats">
          <span title="dispositivos de captura">{stats.devices} dev</span>
          <span title="streams TCP reensamblados">{stats.streams} str</span>
          <span title="paquetes por segundo">{Math.round(stats.packetsPerSec)} pps</span>
          <span title="bytes capturados">{fmtCompact(stats.bytes)}</span>
          <span title="huecos de reensamblado">g{stats.gaps}</span>
        </div>
        <div className="log" title={lastLog}>
          {lastLog || 'waiting for capture…'}
        </div>
      </footer>
    </div>
  );
}
