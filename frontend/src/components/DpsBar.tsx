import { fmtCompact } from '../format';
import type { CombatMember } from '../types';

interface DpsBarProps {
  member: CombatMember;
  rank: number;
  maxDps: number;
}

export function DpsBar({ member, rank, maxDps }: DpsBarProps) {
  const pct = maxDps > 0 ? Math.max(2, (member.dps / maxDps) * 100) : 0;
  return (
    <div className="row">
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
