import type { Severity, State } from "../types";

const LABELS: Record<State, string> = {
  live: "LIVE",
  stale: "STALE",
  source_down: "SOURCE DOWN",
};

export function StateBadge({ state, severity }: { state: State; severity?: Severity }) {
  const tone = severity ? `sev-${severity}` : state;
  return <span className={`badge badge-${tone}`}>{LABELS[state]}</span>;
}
