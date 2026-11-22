import type { CSSProperties } from 'react';
import { classEmblem } from '../classes';
import { fmtCompact } from '../format';
import type { Metric, MetricMember } from '../types';

interface StatBarProps {
  member: MetricMember;
  rank: number;
  maxRate: number;
  // Porcentaje (0–100) del total del grupo para la métrica activa.
  share: number;
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

// StatBar pinta una fila compacta de una sola línea: puesto, emblema, nombre,
// ritmo por segundo, total y porcentaje del grupo. El relleno de color del fondo
// escala con el ritmo. La fila entera abre el desglose del miembro.
export function StatBar({ member, rank, maxRate, share, metric, onSelect }: StatBarProps) {
  // Mínimo del 2% para que la barra siga siendo visible en los puestos bajos.
  const pct = maxRate > 0 ? Math.max(2, (member.perSecond / maxRate) * 100) : 0;
  const emblem = classEmblem(member.class);
  const color = member.color || '#8b94a7';

  const select = () => onSelect(member);

  return (
    <div
      className={`row metric-${metric}${member.self ? ' row-self' : ''}`}
      role="button"
      tabIndex={0}
      title={`${member.name}${member.class ? ` · ${member.class}` : ''} — view breakdown`}
      style={{ '--class-color': color } as CSSProperties}
      onClick={select}
      onKeyDown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault();
          select();
        }
      }}
    >
      <div className="bar-fill" style={{ width: `${pct}%` }} />
      <span className="rank">{rank}</span>
      {emblem ? (
        <img className="class-emblem" src={emblem} alt="" loading="lazy" draggable={false} />
      ) : (
        <span />
      )}
      <span className="name">
        {member.name}
        {member.self && <span className="you">YOU</span>}
      </span>
      <span className="rate" title={rateTitle(metric)}>
        {fmtCompact(member.perSecond)}
      </span>
      <span className="total" title={totalTitle(metric)}>
        {fmtCompact(member.value)}
      </span>
      <span className="share" title="Share of group total">
        {share.toFixed(0)}%
      </span>
    </div>
  );
}
