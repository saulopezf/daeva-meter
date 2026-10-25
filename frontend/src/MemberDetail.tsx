import { useCallback, useEffect, useRef, useState } from 'react';
import { Events } from '@wailsio/runtime';
import { App as Backend } from '../bindings/daeva-meter';
import { fmtCompact } from './format';
import type { MemberDetail as MemberDetailData, SkillStat } from './types';

interface MemberDetailProps {
  memberKey: string;
  initialName: string;
}

function skillLabel(skill: SkillStat): string {
  if (skill.name) return skill.name;
  return skill.id > 0 ? `Skill #${skill.id}` : 'Unknown';
}

// MemberDetail es la ventana que se abre al pulsar una fila de la tabla de DPS.
// Consulta el desglose del miembro y lo refresca con cada evento de combate.
export default function MemberDetail({ memberKey, initialName }: MemberDetailProps) {
  const [detail, setDetail] = useState<MemberDetailData | null>(null);
  const [waiting, setWaiting] = useState(false);
  const [error, setError] = useState('');

  const alive = useRef(true);
  const busy = useRef(false);

  const load = useCallback(async () => {
    if (busy.current) return;
    busy.current = true;
    try {
      const next = await Backend.MemberDetail(memberKey);
      if (!alive.current) return;
      if (next.found) {
        setDetail(next);
        setWaiting(false);
      } else {
        setWaiting(true);
      }
      setError('');
    } catch (e) {
      if (alive.current) setError(String(e));
    } finally {
      busy.current = false;
    }
  }, [memberKey]);

  useEffect(() => {
    alive.current = true;
    void load();
    const off = Events.On('combat:update', () => {
      void load();
    });
    return () => {
      alive.current = false;
      off();
    };
  }, [load]);

  const skills = detail?.skills ?? [];
  const maxSkillDamage = Math.max(...skills.map((s) => s.damage), 1);
  const critRate = detail && detail.hits > 0 ? (detail.crits / detail.hits) * 100 : 0;
  const name = detail?.name || initialName || 'Unknown';
  const color = detail?.color || '#8b94a7';
  const status = waiting ? 'WAITING' : detail?.active ? 'LIVE' : detail ? 'ENDED' : 'WAITING';

  return (
    <div className="app">
      <header className="detail-head">
        <div className="detail-title">
          <span className="name" style={{ color }}>
            {name}
          </span>
          {detail?.self && <span className="you">YOU</span>}
          {detail?.class && (
            <span className="cls" style={{ color }}>
              {detail.class}
            </span>
          )}
          <span className={!waiting && detail?.active ? 'live-tag' : 'mock'}>{status}</span>
        </div>

        <div className="detail-stats">
          <div className="detail-stat">
            <span className="label">Damage</span>
            <span className="value">{fmtCompact(detail?.damage ?? 0)}</span>
          </div>
          <div className="detail-stat">
            <span className="label">DPS</span>
            <span className="value">{fmtCompact(detail?.dps ?? 0)}</span>
          </div>
          <div className="detail-stat">
            <span className="label">Hits</span>
            <span
              className="value"
              title={`${detail?.hits ?? 0} damage events + ${detail?.extraHits ?? 0} extra hits`}
            >
              {detail?.totalHits ?? 0}
            </span>
          </div>
          <div className="detail-stat">
            <span className="label">Crits</span>
            <span className="value">
              {detail?.crits ?? 0}
              <small>{critRate.toFixed(0)}%</small>
            </span>
          </div>
          <div className="detail-stat">
            <span className="label">Duration</span>
            <span className="value">{(detail?.elapsed ?? 0).toFixed(0)}s</span>
          </div>
        </div>
      </header>

      {error && <div className="error">{error}</div>}
      {waiting && (
        <div className="detail-banner">
          waiting for this member to deal damage in the current encounter…
        </div>
      )}

      <main className="detail-main">
        {skills.length === 0 ? (
          <div className="empty">
            {detail ? 'no skill damage recorded yet…' : 'waiting for combat data…'}
          </div>
        ) : (
          skills.map((skill) => {
            const pct = Math.max(2, (skill.damage / maxSkillDamage) * 100);
            const skillCritRate =
              skill.hits > 0 ? (skill.crits / skill.hits) * 100 : 0;
            return (
              <div className="skill-row" key={skill.id}>
                <div className="skill-top">
                  <span className="skill-name" title={skillLabel(skill)}>
                    {skillLabel(skill)}
                  </span>
                  <span className="skill-damage">{fmtCompact(skill.damage)}</span>
                  <span className="skill-share">{skill.share.toFixed(1)}%</span>
                </div>
                <div className="track skill-track">
                  <div
                    className="fill"
                    style={{ width: `${pct}%`, background: color }}
                  />
                </div>
                <div className="skill-meta">
                  <span>{fmtCompact(skill.dps)}/s</span>
                  <span>
                    {skill.totalHits} hits
                    {skill.extraHits > 0 &&
                      ` (${skill.hits}+${skill.extraHits})`}
                  </span>
                  <span>
                    {skill.crits} crits · {skillCritRate.toFixed(0)}%
                  </span>
                  <span>avg {fmtCompact(skill.avgHit)}</span>
                </div>
              </div>
            );
          })
        )}
      </main>

      <footer>
        <div className="log">
          Hits include extra multi-hits; crits are counted per damage event.
        </div>
      </footer>
    </div>
  );
}
