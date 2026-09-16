import type { State } from "../types";

const LABELS: Record<State, string> = {
  live: "LIVE",
  stale: "STALE",
  source_down: "SOURCE DOWN",
};

// StateBadge renders a panel's live/stale/source_down state as a colored pill.
// Every panel shows all three states cleanly through this one component.
export function StateBadge({ state }: { state: State }) {
  return <span className={`badge badge-${state}`}>{LABELS[state]}</span>;
}
