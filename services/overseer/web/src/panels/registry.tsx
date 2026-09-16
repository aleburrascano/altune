import type { FC } from "react";
import type { PanelProps } from "../types";
import { GenericPanel } from "./GenericPanel";
import { LiveActivityPanel } from "./LiveActivityPanel";

// panels is the frontend panel registry keyed by bucket id, mirroring the Go
// registry. Adding a bucket panel is one line here; the app core references no
// concrete panel. An id with no bespoke panel falls back to GenericPanel, so a
// backend-only bucket appears immediately and is upgraded later.
export const panels: Record<string, FC<PanelProps>> = {
  liveactivity: LiveActivityPanel as FC<PanelProps>,
};

// panelFor returns the component for a bucket id, or the generic fallback.
export function panelFor(id: string): FC<PanelProps> {
  return panels[id] ?? GenericPanel;
}
