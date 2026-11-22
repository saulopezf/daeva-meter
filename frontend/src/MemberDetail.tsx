import { useCallback, useEffect, useRef, useState } from 'react';
import { Events } from '@wailsio/runtime';
import { App as Backend } from '../bindings/daeva-meter';
import { classEmblem } from './classes';
import { TitleBar } from './components/TitleBar';
import { fmtCompact } from './format';
import { useOpacity } from './useOpacity';
import type { MemberDetail as MemberDetailData, Metric, SkillStat } from './types';

interface MemberDetailProps {
  memberKey: string;
  initialName: string;
  encounterID: number;
  metric: Metric;
}

function metricLabel(metric: Metric): string {
  if (metric === 'healing') return 'Healing';
  if (metric === 'taken') return 'Damage Taken';
  return 'Damage';
}

function rateLabel(metric: Metric): string {
  if (metric === 'healing') return 'HPS';
  if (metric === 'taken') return 'DTPS';
  return 'DPS';
}

function waitingText(metric: Metric): string {
  if (metric === 'healing') return 'waiting for this member to heal in this encounter…';
  if (metric === 'taken') return 'waiting for this member to take damage in this encounter…';
  return 'waiting for this member to deal damage in the current encounter…';
}

function emptyText(metric: Metric, hasDetail: boolean): string {
  if (!hasDetail) return 'waiting for combat data…';
  if (metric === 'healing') return 'no healing recorded yet…';
  if (metric === 'taken') return 'no damage taken recorded yet…';
  return 'no skill damage recorded yet…';
}

function footerText(metric: Metric): string {
  if (metric === 'healing') {
    return 'Healing is counted per heal event; crits are counted when the game marks them.';
  }
  if (metric === 'taken') {
    return 'Damage taken is grouped by source skill; hits are counted per damage event.';
  }
  return 'Hits include extra multi-hits; crits are counted per damage event.';
}

function skillLabel(skill: SkillStat): string {
  if (skill.name) return skill.name;
  return skill.id > 0 ? `Skill #${skill.id}` : 'Unknown';
}

// MemberDetail es la ventana que se abre al pulsar una fila de cualquiera de las
// tablas. Consulta el desglose del miembro para la métrica y el combate
// indicados; si el combate es el actual, se refresca con cada evento de combate.
export default function MemberDetail({
  memberKey,
  initialName,
  encounterID,
  metric,
}: MemberDetailProps) {
  useOpacity();
  const [detail, setDetail] = useState<MemberDetailData | null>(null);
  const [waiting, setWaiting] = useState(false);
  const [error, setError] = useState('');

  const alive = useRef(true);
  const busy = useRef(false);

  const load = useCallback(async () => {
    if (busy.current) return;
    busy.current = true;
    try {
      const next = await Backend.MemberDetail(encounterID, memberKey, metric);
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
  }, [encounterID, memberKey, metric]);

  useEffect(() => {
    alive.current = true;
    void load();
    // Un combate archivado es estático; solo el actual se refresca en vivo.
    if (encounterID !== 0) {
      return () => {
        alive.current = false;
      };
    }
    const off = Events.On('combat:update', () => {
      void load();
    });
    return () => {
      alive.current = false;
      off();
    };
  }, [load, encounterID]);

  const skills = detail?.skills ?? [];
  const maxSkillDamage = Math.max(...skills.map((s) => s.damage), 1);
  const critRate = detail && detail.hits > 0 ? (detail.crits / detail.hits) * 100 : 0;
  const name = detail?.name || initialName || 'Unknown';
  const color = detail?.color || '#8b94a7';
  const emblem = classEmblem(detail?.class ?? '');
  const status = waiting
    ? 'WAITING'
    : encounterID !== 0
      ? 'HISTORY'
      : detail?.active
        ? 'LIVE'
        : detail
          ? 'ENDED'
          : 'WAITING';

  return (
    <div className="app">
      <TitleBar />

      <header className="detail-head">
        <div className="detail-title">
          {emblem && (
            <img
              className="class-emblem detail-emblem"
              src={emblem}
              alt=""
              title={detail?.class}
              draggable={false}
            />
          )}
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
            <span className="label">{metricLabel(metric)}</span>
            <span className="value">{fmtCompact(detail?.damage ?? 0)}</span>
          </div>
          <div className="detail-stat">
            <span className="label">{rateLabel(metric)}</span>
            <span className="value">{fmtCompact(detail?.dps ?? 0)}</span>
          </div>
          <div className="detail-stat">
            <span className="label">Hits</span>
            <span
              className="value"
              title={`${detail?.hits ?? 0} events + ${detail?.extraHits ?? 0} extra hits`}
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
      {waiting && <div className="detail-banner">{waitingText(metric)}</div>}

      <main className="detail-main">
        {skills.length === 0 ? (
          <div className="empty">{emptyText(metric, detail !== null)}</div>
        ) : (
          skills.map((skill) => {
            const pct = Math.max(2, (skill.damage / maxSkillDamage) * 100);
            const skillCritRate = skill.hits > 0 ? (skill.crits / skill.hits) * 100 : 0;
            return (
              <div className="skill-row" key={skill.key || String(skill.id)}>
                <div className="bar skill-bar">
                  <div
                    className="bar-fill"
                    style={{ width: `${pct}%`, background: color }}
                  />
                  <div className="bar-content skill-content">
                    <span className="skill-icon-wrap">
                      {skill.icon ? (
                        <img
                          className="skill-icon"
                          src={`/${skill.icon}`}
                          alt=""
                          loading="lazy"
                          draggable={false}
                        />
                      ) : (
                        <span className="skill-icon-fallback" aria-hidden="true" />
                      )}
                    </span>
                    <span className="skill-name" title={skillLabel(skill)}>
                      {skillLabel(skill)}
                    </span>
                    <span className="skill-damage">{fmtCompact(skill.damage)}</span>
                    <span className="skill-share">{skill.share.toFixed(1)}%</span>
                  </div>
                </div>
                <div className="skill-meta">
                  <span>{fmtCompact(skill.dps)}/s</span>
                  <span>
                    {skill.totalHits} hits
                    {skill.extraHits > 0 && ` (${skill.hits}+${skill.extraHits})`}
                  </span>
                  <span>
                    {skill.crits} crits · {skillCritRate.toFixed(0)}%
                  </span>
                  <span>avg {fmtCompact(skill.avgHit)}</span>
                  {skill.source && <span className="skill-source">from {skill.source}</span>}
                </div>
              </div>
            );
          })
        )}
      </main>

      <footer>
        <div className="log">{footerText(metric)}</div>
      </footer>
    </div>
  );
}
