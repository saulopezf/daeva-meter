import { useCallback, useEffect, useState } from 'react';
import { Events } from '@wailsio/runtime';
import { App as Backend } from '../bindings/daeva-meter';
import { DpsBar } from './components/DpsBar';
import { TitleBar } from './components/TitleBar';
import { fmtClock, fmtCompact } from './format';
import type { CaptureState, CombatSnapshot, CombatMember, Stats } from './types';
import { useDynamicWindowHeight } from './useDynamicWindowHeight';

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
  boss: '',
  bossId: 0,
  isBoss: false,
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
  const appRef = useDynamicWindowHeight<HTMLDivElement>();
  const [state, setState] = useState<CaptureState | null>(null);
  const [stats, setStats] = useState<Stats>(EMPTY_STATS);
  const [combat, setCombat] = useState<CombatSnapshot>(EMPTY_COMBAT);
  const [logs, setLogs] = useState<string[]>([]);

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

  // openMember abre (o enfoca) la ventana de desglose del miembro pulsado.
  const openMember = useCallback(
    async (member: CombatMember) => {
      if (!member.key) return;
      try {
        await Backend.OpenMemberWindow(member.key, member.name);
      } catch (e) {
        pushLog(String(e));
      }
    },
    [pushLog],
  );

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
  // El panel de diagnóstico solo se pinta en compilaciones de desarrollo. Vite
  // elimina el bloque del bundle de producción, y el backend confirma el modo
  // con el mismo build tag que controla el log de sesión.
  const dev = import.meta.env.MODE === 'development' && state?.dev === true;
  // Cabecera de combate: solo el tipo (jefe o fight normal) y el tiempo.
  const fightState = combat.active ? 'active' : members.length > 0 ? 'ended' : 'idle';
  const fightKind = combat.isBoss ? 'boss' : 'fight';

  return (
    <div className="app app-auto" ref={appRef}>
      <TitleBar />

      {dev && (
        <details className="dev-panel">
          <summary>dev</summary>
          <div className="dev-body">
            <div className="dev-row">
              <span className={`badge badge-${status}`}>
                <i className="dot" />
                {STATUS_LABEL[status]}
              </span>
              <span className="port">tcp/13328</span>
              {state?.logPath && (
                <span className="dev-path" title={state.logPath}>
                  {state.logPath}
                </span>
              )}
            </div>
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
          </div>
        </details>
      )}

      {state?.error && <div className="error">{state.error}</div>}

      <div className={`fight-bar fight-bar-${fightState}`}>
        <span className={`fight-kind fight-kind-${fightState === 'idle' ? 'idle' : fightKind}`}>
          {fightState !== 'idle' &&
            (combat.isBoss ? (
              <svg className="fight-icon" viewBox="0 0 16 16" aria-hidden="true">
                <path d="M3 10.5V5l3 2 2-3.5 2 3.5 3-2v5.5" />
                <path d="M3 12.5h10" />
              </svg>
            ) : (
              <svg className="fight-icon" viewBox="0 0 16 16" aria-hidden="true">
                <path d="M2.5 2.5l8 8M10.5 10.5l3 3M13.5 2.5l-8 8M5.5 10.5l-3 3" />
              </svg>
            ))}
          {fightState === 'idle' ? 'IDLE' : combat.isBoss ? 'BOSS' : 'FIGHT'}
        </span>
        <span className="fight-time">
          {fmtClock(combat.elapsed)}
          {combat.active && <i className="fight-live-dot" />}
        </span>
      </div>

      <main className="rows">
        {members.length === 0 ? (
          <div className="empty">waiting for combat data…</div>
        ) : (
          members.map((m, i) => (
            <DpsBar
              key={m.key}
              member={m}
              rank={i + 1}
              maxDps={maxDps}
              onSelect={(member) => void openMember(member)}
            />
          ))
        )}
      </main>
    </div>
  );
}
