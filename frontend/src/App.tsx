import { useCallback, useEffect, useState } from 'react';
import { Events } from '@wailsio/runtime';
import { App as Backend } from '../bindings/daeva-meter';
import { StatBar } from './components/StatBar';
import { TitleBar } from './components/TitleBar';
import { fmtClock, fmtCompact } from './format';
import type {
  CaptureState,
  CombatSnapshot,
  EncounterSummary,
  Metric,
  MetricMember,
  Stats,
} from './types';
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
  id: 0,
  active: false,
  startedAt: 0,
  elapsed: 0,
  boss: '',
  bossId: 0,
  isBoss: false,
  selfId: 0,
  self: '',
  members: [],
  healing: [],
  taken: [],
  party: [],
};

const TABS: { id: Metric; label: string }[] = [
  { id: 'damage', label: 'Damage' },
  { id: 'healing', label: 'Healing' },
  { id: 'taken', label: 'Damage Taken' },
];

// Estado global de la captura derivado del backend y de las estadísticas.
type Status = 'connecting' | 'live' | 'listening' | 'stopped' | 'error';

const STATUS_LABEL: Record<Status, string> = {
  connecting: 'CONNECTING',
  live: 'LIVE',
  listening: 'LISTENING',
  stopped: 'STOPPED',
  error: 'ERROR',
};

// metricRows adapta la tabla de daño (que usa damage/dps) al tipo común de fila.
function metricRows(snapshot: CombatSnapshot, metric: Metric): MetricMember[] {
  if (metric === 'healing') return snapshot.healing ?? [];
  if (metric === 'taken') return snapshot.taken ?? [];
  return (snapshot.members ?? []).map((m) => ({
    id: m.id,
    key: m.key,
    name: m.name,
    class: m.class,
    color: m.color,
    value: m.damage,
    perSecond: m.dps,
    hits: m.hits,
    crits: m.crits,
    self: m.self,
  }));
}

function emptyLabel(metric: Metric): string {
  if (metric === 'healing') return 'waiting for healing data…';
  if (metric === 'taken') return 'waiting for damage taken…';
  return 'waiting for combat data…';
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

// App es la raíz de la interfaz. Consulta el estado inicial, se suscribe a los
// eventos del backend y pinta la cabecera, las pestañas de métricas, la tabla y
// el historial de combates de la sesión.
export default function App() {
  const appRef = useDynamicWindowHeight<HTMLDivElement>();
  const [state, setState] = useState<CaptureState | null>(null);
  const [stats, setStats] = useState<Stats>(EMPTY_STATS);
  const [live, setLive] = useState<CombatSnapshot>(EMPTY_COMBAT);
  const [pinned, setPinned] = useState<CombatSnapshot | null>(null);
  const [selectedID, setSelectedID] = useState(0);
  const [tab, setTab] = useState<Metric>('damage');
  const [history, setHistory] = useState<EncounterSummary[]>([]);
  const [logs, setLogs] = useState<string[]>([]);

  // pushLog conserva solo las últimas 5 líneas de log para el pie.
  const pushLog = useCallback((msg: string) => {
    setLogs((prev) => [...prev.slice(-4), msg]);
  }, []);

  // refresh trae el estado completo del backend bajo demanda.
  const refresh = useCallback(async () => {
    try {
      const s = await Backend.CaptureStatus();
      setState(s);
      setStats(s.stats);
      setLive(await Backend.CombatState());
      setHistory((await Backend.CombatHistory()) ?? []);
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
    const offCombat = Events.On('combat:update', (ev) => setLive(ev.data as CombatSnapshot));
    const offHistory = Events.On('combat:history', (ev) =>
      setHistory(ev.data as EncounterSummary[]),
    );
    return () => {
      offStats();
      offLog();
      offCombat();
      offHistory();
    };
  }, [refresh, pushLog]);

  // selectEncounter fija la vista en un combate anterior. El id 0 vuelve al
  // combate en vivo.
  const selectEncounter = useCallback(
    async (id: number) => {
      if (id === 0) {
        setSelectedID(0);
        setPinned(null);
        return;
      }
      try {
        const snap = await Backend.CombatStateFor(id);
        setSelectedID(id);
        setPinned(snap);
      } catch (e) {
        pushLog(String(e));
      }
    },
    [pushLog],
  );

  // openMember abre (o enfoca) la ventana de desglose del miembro pulsado para
  // la pestaña y el combate que se están viendo.
  const openMember = useCallback(
    async (member: MetricMember) => {
      if (!member.key) return;
      try {
        await Backend.OpenMemberWindow(selectedID, member.key, member.name, tab);
      } catch (e) {
        pushLog(String(e));
      }
    },
    [pushLog, selectedID, tab],
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

  const view = selectedID === 0 ? live : pinned ?? EMPTY_COMBAT;
  const rows = metricRows(view, tab);
  // Máximo del ritmo para escalar las barras de forma relativa al primero.
  const maxRate = Math.max(...rows.map((m) => m.perSecond), 1);
  const lastLog = logs.length > 0 ? logs[logs.length - 1] : '';
  // El panel de diagnóstico solo se pinta en compilaciones de desarrollo. Vite
  // elimina el bloque del bundle de producción, y el backend confirma el modo
  // con el mismo build tag que controla el log de sesión.
  const dev = import.meta.env.MODE === 'development' && state?.dev === true;
  const isHistory = selectedID !== 0;
  const fightState = isHistory
    ? 'ended'
    : view.active
      ? 'active'
      : rows.length > 0
        ? 'ended'
        : 'idle';
  const fightKind = view.isBoss ? 'boss' : 'fight';

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
            (view.isBoss ? (
              <svg className="fight-icon" viewBox="0 0 16 16" aria-hidden="true">
                <path d="M3 10.5V5l3 2 2-3.5 2 3.5 3-2v5.5" />
                <path d="M3 12.5h10" />
              </svg>
            ) : (
              <svg className="fight-icon" viewBox="0 0 16 16" aria-hidden="true">
                <path d="M2.5 2.5l8 8M10.5 10.5l3 3M13.5 2.5l-8 8M5.5 10.5l-3 3" />
              </svg>
            ))}
          {fightState === 'idle' ? 'IDLE' : view.isBoss ? 'BOSS' : 'FIGHT'}
        </span>
        {view.isBoss && view.boss && (
          <span className="fight-boss" title={view.boss}>
            {view.boss}
          </span>
        )}
        <span className="fight-time">
          {fmtClock(view.elapsed)}
          {!isHistory && view.active && <i className="fight-live-dot" />}
        </span>
        <label className="history-picker" title="Combates anteriores de la sesión">
          <span className="history-label">{isHistory ? 'HISTORY' : 'LIVE'}</span>
          <select
            className="history-select"
            value={String(selectedID)}
            onChange={(e) => void selectEncounter(Number(e.target.value))}
          >
            <option value="0">Live</option>
            {history.map((entry) => (
              <option key={entry.id} value={String(entry.id)}>
                {historyLabel(entry)}
              </option>
            ))}
          </select>
        </label>
      </div>

      <div className="tabs" role="tablist" aria-label="Combat metrics">
        {TABS.map((item) => (
          <button
            key={item.id}
            type="button"
            role="tab"
            aria-selected={tab === item.id}
            className={`tab${tab === item.id ? ' tab-active' : ''}`}
            onClick={() => setTab(item.id)}
          >
            {item.label}
          </button>
        ))}
      </div>

      <main className="rows">
        {rows.length === 0 ? (
          <div className="empty">
            {isHistory ? 'no data for this fight…' : emptyLabel(tab)}
          </div>
        ) : (
          rows.map((m, i) => (
            <StatBar
              key={m.key}
              member={m}
              rank={i + 1}
              maxRate={maxRate}
              metric={tab}
              onSelect={(member) => void openMember(member)}
            />
          ))
        )}
      </main>
    </div>
  );
}
