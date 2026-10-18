// Tipos que replican sniffer.Stats / main.CaptureState / aion2.Snapshot de Go.
// Deben mantenerse sincronizados con los structs del backend que se serializan
// a JSON.

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
  name: string;
  class: string;
  color: string;
  damage: number;
  dps: number;
  hits: number;
  crits: number;
  self: boolean;
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
