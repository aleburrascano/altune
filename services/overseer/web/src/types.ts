// The seam contract mirrored from the Go core (internal/core/snapshot.go). These
// types are hand-written to match the JSON the API emits; every panel builds
// against them.

export type State = "live" | "stale" | "source_down";

export type Reason = "auth" | "throttled" | "degraded" | "down" | "connecting";

// Severity is the bucket's health grade, judged from its own payload. It is
// deliberately independent of State: State says how fresh the data is, severity
// says how bad it is. A bucket whose source is fresh can be critical, and one
// serving last-known data flagged source_down can still be ok.
export type Severity = "ok" | "warn" | "critical";

export interface Snapshot<D = unknown> {
  id: string;
  title: string;
  state: State;
  reason?: Reason;
  // severity and headline are the health half of the envelope; headline is the
  // one figure that matters for this bucket — the number severity grades.
  severity: Severity;
  headline: string;
  updatedAt: string;
  data: D;
  spark?: SeriesPoint[];
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
