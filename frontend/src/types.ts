// Tipos de la interfaz. Los modelos equivalentes del backend se generan en
// frontend/bindings/daeva-meter/; se mantienen aquí para no acoplar la UI a los
// bindings generados.

export interface Stats {
  devices: number;
  packets: number;
  bytes: number;
  payloadBytes: number;
  streams: number;
  gaps: number;
  packetsPerSec: number;
  bytesPerSec: number;
  trafficSeen: boolean;
}

export interface CaptureState {
  running: boolean;
  error: string;
  stats: Stats;
  logPath: string;
}

// Una fila de la tabla de DPS, procedente de aion2.MemberStat.
export interface CombatMember {
  id: number;
  key: string;
  name: string;
  class: string;
  color: string;
  damage: number;
  dps: number;
  hits: number;
  crits: number;
  self: boolean;
}

// El desglose de una habilidad, procedente de aion2.SkillStat.
export interface SkillStat {
  id: number;
  name: string;
  damage: number;
  dps: number;
  share: number;
  hits: number;
  extraHits: number;
  totalHits: number;
  crits: number;
  avgHit: number;
}

// El detalle de un miembro, procedente de aion2.MemberDetail.
export interface MemberDetail {
  key: string;
  name: string;
  class: string;
  color: string;
  self: boolean;
  found: boolean;
  active: boolean;
  elapsed: number;
  damage: number;
  dps: number;
  hits: number;
  extraHits: number;
  totalHits: number;
  crits: number;
  skills: SkillStat[] | null;
}

// El encuentro actual, procedente de aion2.Snapshot.
export interface CombatSnapshot {
  active: boolean;
  elapsed: number;
  selfId: number;
  self: string;
  members: CombatMember[] | null;
  party: string[] | null;
}
