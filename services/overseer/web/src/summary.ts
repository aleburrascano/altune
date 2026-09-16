import type { Snapshot } from "./types";

// summarize derives a single glanceable headline from a bucket's snapshot data for
// the overview grid. It is intentionally generic: the overview core references no
// concrete bucket (the epic's "additive on both sides" rule), so the headline is
// inferred from the *shape* of `data`, never hand-mapped per bucket. When there is
// no obvious headline the summary is empty and the caller shows the state alone —
// so a bucket with an unfamiliar payload still reads cleanly, never crashes.
export function summarize(snapshot: Snapshot): string {
  return headline(snapshot.data);
}

function headline(data: unknown): string {
  if (data == null) return "";
  if (typeof data === "string") return truncate(data.trim());
  if (typeof data === "number" || typeof data === "boolean") return String(data);
  if (Array.isArray(data)) return `${data.length} items`;
  if (typeof data === "object") return fromObject(data as Record<string, unknown>);
  return "";
}

function fromObject(obj: Record<string, unknown>): string {
  // Prefer the largest non-empty top-level collection — "128 events", "5 routes" —
  // which is the most glanceable signal for most buckets.
  let best: { key: string; len: number } | null = null;
  for (const [key, value] of Object.entries(obj)) {
    if (Array.isArray(value) && value.length > 0 && (best === null || value.length > best.len)) {
      best = { key, len: value.length };
    }
  }
  if (best !== null) return `${best.len} ${humanize(best.key)}`;

  // Otherwise the first meaningful scalar — "score: 0.98", "enabled: true".
  for (const [key, value] of Object.entries(obj)) {
    if (typeof value === "number" || typeof value === "boolean") return `${humanize(key)}: ${value}`;
    if (typeof value === "string" && value.trim() !== "") return `${humanize(key)}: ${truncate(value.trim())}`;
  }
  return "";
}

// humanize turns a data key into words: "inFlight" -> "in flight",
// "queue_depth" -> "queue depth".
function humanize(key: string): string {
  return key
    .replace(/([a-z0-9])([A-Z])/g, "$1 $2")
    .replace(/[_-]+/g, " ")
    .trim()
    .toLowerCase();
}

function truncate(value: string, max = 56): string {
  return value.length > max ? `${value.slice(0, max - 1)}…` : value;
}
