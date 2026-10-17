import { useCallback, useEffect, useState } from 'react';
import { CaptureStatus, CombatState, StartCapture, StopCapture } from '../wailsjs/go/main/App';
import { EventsOff, EventsOn } from '../wailsjs/runtime/runtime';
import { DpsBar } from './components/DpsBar';
import { fmtCompact } from './format';
import type { CaptureState, CombatSnapshot, CombatMember, Stats } from './types';

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

type Status = 'connecting' | 'live' | 'listening' | 'stopped' | 'error';

const STATUS_LABEL: Record<Status, string> = {
  connecting: 'CONNECTING',
  live: 'LIVE',
  listening: 'LISTENING',
  stopped: 'STOPPED',
  error: 'ERROR',
};

export default function App() {
  const [state, setState] = useState<CaptureState | null>(null);
  const [stats, setStats] = useState<Stats>(EMPTY_STATS);
  const [combat, setCombat] = useState<CombatSnapshot>(EMPTY_COMBAT);
  const [logs, setLogs] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);

  const pushLog = useCallback((msg: string) => {
    setLogs((prev) => [...prev.slice(-4), msg]);
  }, []);

  const refresh = useCallback(async () => {
    try {
      const s = await CaptureStatus();
      setState(s);
      setStats(s.stats);
      setCombat(await CombatState());
    } catch (e) {
      pushLog(String(e));
    }
  }, [pushLog]);

  useEffect(() => {
    void refresh();
    EventsOn('capture:stats', (st: Stats) => setStats(st));
    EventsOn('capture:log', (msg: string) => pushLog(msg));
    EventsOn('combat:update', (snap: CombatSnapshot) => setCombat(snap));
    return () => {
      EventsOff('capture:stats');
      EventsOff('capture:log');
      EventsOff('combat:update');
    };
  }, [refresh, pushLog]);

  const toggle = useCallback(async () => {
    setBusy(true);
    try {
      if (state?.running) {
        StopCapture();
      } else {
        await StartCapture();
      }
    } catch (e) {
      pushLog(String(e));
    } finally {
      await refresh();
      setBusy(false);
    }
  }, [state, refresh, pushLog]);

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
          <span title="capture devices">{stats.devices} dev</span>
          <span title="reassembled TCP streams">{stats.streams} str</span>
          <span title="packets per second">{Math.round(stats.packetsPerSec)} pps</span>
          <span title="bytes captured">{fmtCompact(stats.bytes)}</span>
          <span title="reassembly gaps">g{stats.gaps}</span>
        </div>
        <div className="log" title={lastLog}>
          {lastLog || 'waiting for capture…'}
        </div>
      </footer>
    </div>
  );
}
