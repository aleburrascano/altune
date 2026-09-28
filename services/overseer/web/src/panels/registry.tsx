import type { FC } from "react";
import type { PanelProps } from "../types";
import { GenericPanel } from "./GenericPanel";

const modules = import.meta.glob("./*.panel.tsx", { eager: true }) as Record<
  string,
  { default: FC<PanelProps> }
>;

function bucketId(path: string): string {
  return path.slice(path.lastIndexOf("/") + 1).replace(/\.panel\.tsx$/, "");
}

export const panels: Record<string, FC<PanelProps>> = Object.fromEntries(
  Object.entries(modules).map(([path, mod]) => [bucketId(path), mod.default]),
);

export function panelFor(id: string): FC<PanelProps> {
  return panels[id] ?? GenericPanel;
}
