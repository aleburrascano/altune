import type { Range } from "./types";

// Client-side route paths for the app. The paths are base-relative — the router's
// basename (Vite's `/overseer/` in prod, `/` in dev) is applied by React Router, so
// these stay clean and the app respects the mount prefix without hard-coding it.

export const overviewPath = "/";

const RANGES: readonly Range[] = ["1h", "24h", "7d"];
const DEFAULT_RANGE: Range = "1h";

export function bucketPath(id: string, range?: Range): string {
  const base = `/bucket/${encodeURIComponent(id)}`;
  return range ? `${base}?range=${range}` : base;
}

export function corrPath(id: string): string {
  return `/corr/${encodeURIComponent(id)}`;
}

export function parseRange(v: string | null): Range {
  return (RANGES as readonly string[]).includes(v ?? "") ? (v as Range) : DEFAULT_RANGE;
}

// routerBasename normalizes Vite's BASE_URL into a React Router basename: a leading
// slash, no trailing slash ("/overseer/" -> "/overseer", "/" -> "/").
export function routerBasename(baseURL: string): string {
  const trimmed = baseURL.replace(/\/+$/, "");
  return trimmed === "" ? "/" : trimmed;
}
