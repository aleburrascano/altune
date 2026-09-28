
export type State = "live" | "stale" | "source_down";

export type Reason = "auth" | "throttled" | "degraded" | "down" | "connecting";

export type Severity = "ok" | "warn" | "critical";

export interface Snapshot<D = unknown> {
  id: string;
  title: string;
  state: State;
  reason?: Reason;
  severity: Severity;
  headline: string;
  updatedAt: string;
  data: D;
  spark?: SeriesPoint[];
}

export interface Signal {
  at: string;
  kind: string;
  text: string;
  corrId?: string;
}

export type Range = "1h" | "24h" | "7d";

export interface SeriesPoint {
  at: string;
  v: number;
  min?: number;
  max?: number;
}

export interface SeriesResponse {
  bucket: string;
  range: Range;
  series: Record<string, SeriesPoint[]>;
}

export interface CredentialHealth {
  ok: boolean;
  lastRefresh?: string;
  consecutiveFailures: number;
  persistFailed: boolean;
  passwordGrant: boolean;
  lastError?: string;
}

export interface OverseerHealth {
  lastCycle?: string;
  bucketsOk: number;
  bucketsFailed: number;
  credential?: CredentialHealth;
}

export type PanelProps<D = unknown> = { snapshot: Snapshot<D>; range: Range };
