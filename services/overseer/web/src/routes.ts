import type { Range } from "./types";


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

export function routerBasename(baseURL: string): string {
  const trimmed = baseURL.replace(/\/+$/, "");
  return trimmed === "" ? "/" : trimmed;
}
