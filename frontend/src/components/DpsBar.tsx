import { classEmblem } from '../classes';
import { fmtCompact } from '../format';
import type { CombatMember } from '../types';

interface DpsBarProps {
  member: CombatMember;
  rank: number;
  maxDps: number;
  onSelect: (member: CombatMember) => void;
}

// DpsBar pinta una fila de la tabla de DPS como una única barra: el relleno de
// color va al fondo y todos los datos (puesto, nombre, clase, DPS y daño) se
// muestran dentro de la propia barra. La fila entera abre el desglose por
// habilidad del miembro.
export function DpsBar({ member, rank, maxDps, onSelect }: DpsBarProps) {
  // La barra se escala sobre maxDps y tiene un mínimo del 2% para seguir siendo
  // visible en los puestos bajos.
  const pct = maxDps > 0 ? Math.max(2, (member.dps / maxDps) * 100) : 0;
  const emblem = classEmblem(member.class);

  const select = () => onSelect(member);

  return (
    <div
      className="row clickable"
      role="button"
      tabIndex={0}
      title="View skill breakdown"
      onClick={select}
      onKeyDown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault();
          select();
        }
      }}
    >
      <div className="bar">
        <div className="bar-fill" style={{ width: `${pct}%`, background: member.color }} />
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
          <span className="cls" style={{ color: member.color }}>
            {member.class}
          </span>
          <span className="dps">
            {fmtCompact(member.dps)}
            <small>/s</small>
          </span>
          <span className="dmg">{fmtCompact(member.damage)}</span>
        </div>
      </div>
    </div>
  );
}
