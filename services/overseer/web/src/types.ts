// The seam contract mirrored from the Go core (internal/core/snapshot.go). These
// types are hand-written to match the JSON the API emits; every panel builds
// against them.

export type State = "live" | "stale" | "source_down";

export interface Snapshot<D = unknown> {
  id: string;
  title: string;
  state: State;
  updatedAt: string;
  data: D;
}

export type PanelProps<D = unknown> = { snapshot: Snapshot<D> };
