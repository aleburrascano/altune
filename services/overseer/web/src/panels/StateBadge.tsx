import type { Severity, State } from "../types";

const LABELS: Record<State, string> = {
  live: "LIVE",
  stale: "STALE",
  source_down: "SOURCE DOWN",
};

// StateBadge renders a bucket's freshness state as a pill whose label always names
// that freshness (LIVE/STALE/SOURCE DOWN). When a severity is supplied the pill is
// colored by health instead of freshness, so a reachable-but-failing bucket reads
// red while its stale/source_down label stays visible beside the health color. With
// no severity (the drill-down panels) it falls back to freshness coloring, unchanged.
export function StateBadge({ state, severity }: { state: State; severity?: Severity }) {
  const tone = severity ? `sev-${severity}` : state;
  return <span className={`badge badge-${tone}`}>{LABELS[state]}</span>;
}
