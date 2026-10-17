// Mirrors sniffer.Stats / main.CaptureState / aion2.Snapshot from Go.

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

// One row on the DPS board, from aion2.MemberStat.
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

// The current encounter, from aion2.Snapshot.
export interface CombatSnapshot {
  active: boolean;
  elapsed: number;
  selfId: number;
  self: string;
  members: CombatMember[] | null;
  party: string[] | null;
}
