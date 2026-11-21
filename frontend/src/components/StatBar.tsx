import { classEmblem } from '../classes';
import { fmtCompact } from '../format';
import type { Metric, MetricMember } from '../types';

interface StatBarProps {
  member: MetricMember;
  rank: number;
  maxRate: number;
  metric: Metric;
  onSelect: (member: MetricMember) => void;
}

function rateTitle(metric: Metric): string {
  if (metric === 'healing') return 'Healing per second';
  if (metric === 'taken') return 'Damage taken per second';
  return 'Damage per second';
}

function totalTitle(metric: Metric): string {
  if (metric === 'healing') return 'Total healing';
  if (metric === 'taken') return 'Total damage taken';
  return 'Total damage';
}

// StatBar pinta una fila de cualquiera de las tres tablas. El relleno de color va
// al fondo y todos los datos se muestran dentro de la propia barra. La fila
// entera abre el desglose del miembro para la métrica activa.
export function StatBar({ member, rank, maxRate, metric, onSelect }: StatBarProps) {
  // La barra se escala sobre el ritmo máximo y tiene un mínimo del 2% para seguir
  // siendo visible en los puestos bajos.
  const pct = maxRate > 0 ? Math.max(2, (member.perSecond / maxRate) * 100) : 0;
  const emblem = classEmblem(member.class);
  const color = member.color || '#8b94a7';

  const select = () => onSelect(member);

  return (
    <div
      className={`row clickable metric-${metric}`}
      role="button"
      tabIndex={0}
      title="View breakdown"
      onClick={select}
      onKeyDown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault();
          select();
        }
      }}
    >
      <div className="bar">
        <div className="bar-fill" style={{ width: `${pct}%`, background: color }} />
        <div className="bar-content">
          <span className="rank">{rank}</span>
          {emblem && (
            <img
              className="class-emblem"
              src={emblem}
              alt=""
              title={member.class}
              loading="lazy"
              draggable={false}
            />
          )}
          <span className="name">{member.name}</span>
          {member.self && <span className="you">YOU</span>}
          <span className="cls" style={{ color }}>
            {member.class}
          </span>
          <span className="dps" title={rateTitle(metric)}>
            {fmtCompact(member.perSecond)}
            <small>/s</small>
          </span>
          <span className="dmg" title={totalTitle(metric)}>
            {fmtCompact(member.value)}
          </span>
        </div>
      </div>
    </div>
  );
}
