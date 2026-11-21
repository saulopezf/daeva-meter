// Tipos de la interfaz. Los modelos equivalentes del backend se generan en
// frontend/bindings/daeva-meter/; se mantienen aquí para no acoplar la UI a los
// bindings generados.

export type Metric = 'damage' | 'healing' | 'taken';

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
  dev: boolean;
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

// Una fila de curación o daño recibido, procedente de aion2.MetricStat.
export interface MetricMember {
  id: number;
  key: string;
  name: string;
  class: string;
  color: string;
  value: number;
  perSecond: number;
  hits: number;
  crits: number;
  self: boolean;
}

// Una entrada del historial de combates, procedente de aion2.EncounterSummary.
export interface EncounterSummary {
  id: number;
  title: string;
  boss: string;
  bossId: number;
  isBoss: boolean;
  active: boolean;
  startedAt: number;
  elapsed: number;
  totalDamage: number;
  totalHealing: number;
  totalTaken: number;
}

// El desglose de una habilidad, procedente de aion2.SkillStat.
export interface SkillStat {
  id: number;
  key: string;
  name: string;
  source: string;
  icon: string;
  damage: number;
  dps: number;
  share: number;
  hits: number;
  extraHits: number;
  totalHits: number;
  crits: number;
  avgHit: number;
}

// El detalle de un miembro, procedente de aion2.MemberDetail. Damage/DPS
// contienen el valor de la métrica seleccionada en Metric.
export interface MemberDetail {
  key: string;
  name: string;
  class: string;
  color: string;
  self: boolean;
  found: boolean;
  active: boolean;
  // El binding generado usa un enum para Metric; se acepta como string porque la
  // interfaz no necesita compararlo.
  metric: string;
  elapsed: number;
  damage: number;
  dps: number;
  hits: number;
  extraHits: number;
  totalHits: number;
  crits: number;
  skills: SkillStat[] | null;
}

// El encuentro actual, procedente de aion2.Snapshot. Members es daño infligido;
// Healing y Taken son las otras dos pestañas.
export interface CombatSnapshot {
  id: number;
  active: boolean;
  startedAt: number;
  elapsed: number;
  boss: string;
  bossId: number;
  isBoss: boolean;
  selfId: number;
  self: string;
  members: CombatMember[] | null;
  healing: MetricMember[] | null;
  taken: MetricMember[] | null;
  party: string[] | null;
}
