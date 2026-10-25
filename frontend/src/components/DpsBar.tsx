import { fmtCompact } from '../format';
import type { CombatMember } from '../types';

interface DpsBarProps {
  member: CombatMember;
  rank: number;
  maxDps: number;
  onSelect: (member: CombatMember) => void;
}

// DpsBar pinta una fila de la tabla de DPS: puesto, nombre, clase, DPS, daño
// total y una barra proporcional al DPS del primer puesto (maxDps). La fila
// entera abre el desglose por habilidad del miembro.
export function DpsBar({ member, rank, maxDps, onSelect }: DpsBarProps) {
  // La barra se escala sobre maxDps y tiene un mínimo del 2% para seguir siendo
  // visible en los puestos bajos.
  const pct = maxDps > 0 ? Math.max(2, (member.dps / maxDps) * 100) : 0;

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
      <div className="row-top">
        <span className="rank">{rank}</span>
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
      <div className="track">
        <div className="fill" style={{ width: `${pct}%`, background: member.color }} />
      </div>
    </div>
  );
}
