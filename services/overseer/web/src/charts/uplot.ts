import type uPlot from "uplot";
import type { SeriesPoint } from "../types";

export const FALLBACK_WIDTH = 480;
const FALLBACK_COLOR = "#5b8cff";

let uPlotLoad: Promise<typeof uPlot> | undefined;

export function loadUPlot(): Promise<typeof uPlot> {
  const pending =
    uPlotLoad ??
    import("uplot").then(
      (mod) => mod.default,
      (err: unknown) => {
        uPlotLoad = undefined;
        throw err;
      },
    );
  uPlotLoad = pending;
  return pending;
}

export function resolveToken(token: string): string {
  const resolved = getComputedStyle(document.documentElement).getPropertyValue(token).trim();
  return resolved || FALLBACK_COLOR;
}

export function translucent(color: string): string {
  return /^#[0-9a-f]{6}$/i.test(color) ? `${color}33` : color;
}

export function formatTime(ms: number, withDate = false): string {
  const at = new Date(ms);
  const time = at.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" });
  if (!withDate) return time;
  return `${at.toLocaleDateString([], { month: "short", day: "numeric" })} ${time}`;
}

export function formatAt(at: string, withDate = false): string {
  return formatTime(Date.parse(at), withDate);
}

const PALETTE_TOKENS = ["--color-accent", "--color-ok", "--color-warn", "--color-critical"];

export function seriesColor(index: number): string {
  return resolveToken(PALETTE_TOKENS[index % PALETTE_TOKENS.length]);
}

export function unionTimestamps(series: { points: SeriesPoint[] }[]): number[] {
  const seen = new Set<number>();
  for (const s of series) {
    for (const p of s.points) {
      const at = Date.parse(p.at) / 1000;
      if (!Number.isNaN(at)) seen.add(at);
    }
  }
  return [...seen].sort((a, b) => a - b);
}

export function alignToTimestamps(points: SeriesPoint[], xs: number[]): (number | null)[] {
  const byTime = new Map<number, number>();
  for (const p of points) {
    const at = Date.parse(p.at) / 1000;
    if (!Number.isNaN(at)) byTime.set(at, p.v);
  }
  return xs.map((x) => byTime.get(x) ?? null);
}

export function toColumns(points: SeriesPoint[]): uPlot.AlignedData {
  return [points.map((p) => Date.parse(p.at) / 1000), points.map((p) => p.v)];
}

export function gridAxes(values?: (u: uPlot, vals: number[]) => (string | null)[]): [uPlot.Axis, uPlot.Axis] {
  const grid = { stroke: resolveToken("--color-border"), width: 1 };
  const axisColor = resolveToken("--color-fg-faint");
  return [
    { stroke: axisColor, grid, ticks: grid },
    { stroke: axisColor, grid, ticks: grid, ...(values ? { values } : {}) },
  ];
}

export function watchResize(el: Element, onResize: () => void): () => void {
  if (typeof ResizeObserver !== "undefined") {
    const observer = new ResizeObserver(onResize);
    observer.observe(el);
    return () => observer.disconnect();
  }
  window.addEventListener("resize", onResize);
  return () => window.removeEventListener("resize", onResize);
}
