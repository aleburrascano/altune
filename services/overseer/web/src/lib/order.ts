import type { Severity, Snapshot, State } from "../types";

const SEVERITY_RANK: Record<Severity, number> = { critical: 0, warn: 1, ok: 2 };
const FRESHNESS_RANK: Record<State, number> = { source_down: 0, stale: 1, live: 2 };

export function compareWorstFirst(a: Snapshot, b: Snapshot): number {
  return (
    SEVERITY_RANK[a.severity] - SEVERITY_RANK[b.severity] ||
    FRESHNESS_RANK[a.state] - FRESHNESS_RANK[b.state] ||
    a.title.localeCompare(b.title)
  );
}

export function worstFirst(a: Snapshot[]): Snapshot[] {
  return [...a].sort(compareWorstFirst);
}

export function neighborBucketId(
  ordered: Snapshot[],
  currentId: string | undefined,
  step: 1 | -1,
): string | undefined {
  if (ordered.length === 0) return undefined;

  const currentIndex = ordered.findIndex((s) => s.id === currentId);
  if (currentIndex === -1) return step === 1 ? ordered[0].id : ordered[ordered.length - 1].id;

  const nextIndex = (currentIndex + step + ordered.length) % ordered.length;
  return ordered[nextIndex].id;
}
