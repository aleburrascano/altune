// The seam contract mirrored from the Go core (internal/core/snapshot.go). These
// types are hand-written to match the JSON the API emits; every panel builds
// against them.

export type State = "live" | "stale" | "source_down";

// Reason names why a snapshot is not live: one of go-api's own failure
// classes ("auth" our credential, "throttled" 429, "degraded" 503, "down"
// transport/other 5xx), or "connecting" for a stream between a drop and its
// next reconnect (added by the stream ticket). Absent when state is live, when
// a source has never yet mirrored, or when the failure behind stale/source_down
// was not classifiable.
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
  // reason is optional: present only when state is not live and the failure
  // behind it was classified. json:",omitempty" on the Go side means the key
  // is absent from the wire rather than sent empty.
  reason?: Reason;
  // severity and headline are the health half of the envelope; headline is the
  // one figure that matters for this bucket — the number severity grades.
  severity: Severity;
  headline: string;
  updatedAt: string;
  data: D;
}

export type PanelProps<D = unknown> = { snapshot: Snapshot<D> };
