import type { FC } from "react";
import type { PanelProps } from "../types";
import { GenericPanel } from "./GenericPanel";

// Panel auto-discovery by file convention. There is no hand-maintained list of
// concrete panel imports here, so the app core references no concrete panel — the
// epic's "additive on the frontend" Must-hold (#1441).
//
// CONVENTION — to add a bespoke bucket panel, add exactly ONE file and edit
// nothing else (no registry.ts, no types.ts, no core):
//
//   web/src/panels/<bucketId>.panel.tsx
//     • `export default` a `React.FC<PanelProps<D>>`
//     • declare its `D` payload type in the SAME file (co-located, not in types.ts)
//
// The registry globs this directory at build time and keys each module by the
// <bucketId> in its filename (see liveactivity.panel.tsx for the worked example).
// A bucket id with no matching file falls back to GenericPanel, so a backend-only
// bucket appears immediately and is upgraded to a bespoke panel later.
//
// Note: the ticket spec writes the glob as './panels/*.panel.tsx' (relative to
// src/). This registry lives in src/panels/, so the equivalent relative glob is
// './*.panel.tsx'.
const modules = import.meta.glob("./*.panel.tsx", { eager: true }) as Record<
  string,
  { default: FC<PanelProps> }
>;

// bucketId derives a bucket id from a panel module path:
// "./liveactivity.panel.tsx" -> "liveactivity".
function bucketId(path: string): string {
  return path.slice(path.lastIndexOf("/") + 1).replace(/\.panel\.tsx$/, "");
}

// panels maps bucket id -> bespoke panel component, discovered from the filenames.
export const panels: Record<string, FC<PanelProps>> = Object.fromEntries(
  Object.entries(modules).map(([path, mod]) => [bucketId(path), mod.default]),
);

// panelFor returns the component for a bucket id, or the generic fallback.
export function panelFor(id: string): FC<PanelProps> {
  return panels[id] ?? GenericPanel;
}
