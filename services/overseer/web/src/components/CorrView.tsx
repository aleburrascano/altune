import { Link, useParams } from "react-router-dom";
import type { Snapshot } from "../types";
import { bucketPath, overviewPath } from "../routes";
import { DataTable, type Column } from "../ui";
import { focusRing } from "../ui/focusRing";

interface CorrelatedSignal {
  bucketId: string;
  bucketTitle: string;
  at: string;
  kind: string;
  text: string;
}

const MAX_DEPTH = 6;

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

function carriesCorrId(entry: Record<string, unknown>, corrId: string): boolean {
  if (typeof entry.corrId === "string" && entry.corrId === corrId) return true;
  const attrs = entry.attrs;
  return isRecord(attrs) && attrs.corr_id === corrId;
}

function firstString(entry: Record<string, unknown>, ...keys: string[]): string | null {
  for (const key of keys) {
    const value = entry[key];
    if (typeof value === "string") return value;
  }
  return null;
}

function signalFrom(bucket: Snapshot, entry: Record<string, unknown>): CorrelatedSignal | null {
  const at = firstString(entry, "at", "time");
  if (!at) return null;
  return {
    bucketId: bucket.id,
    bucketTitle: bucket.title,
    at,
    kind: firstString(entry, "kind", "level") ?? "signal",
    text: firstString(entry, "text", "msg") ?? "",
  };
}

function collect(bucket: Snapshot, value: unknown, corrId: string, depth: number, found: CorrelatedSignal[]): void {
  if (depth > MAX_DEPTH || value == null) return;
  if (Array.isArray(value)) {
    for (const item of value) collect(bucket, item, corrId, depth + 1, found);
    return;
  }
  if (!isRecord(value)) return;
  if (carriesCorrId(value, corrId)) {
    const signal = signalFrom(bucket, value);
    if (signal) found.push(signal);
  }
  for (const nested of Object.values(value)) collect(bucket, nested, corrId, depth + 1, found);
}

function findCorrelatedSignals(buckets: Snapshot[], corrId: string): CorrelatedSignal[] {
  const found: CorrelatedSignal[] = [];
  for (const bucket of buckets) collect(bucket, bucket.data, corrId, 0, found);
  return found.sort((a, b) => Date.parse(a.at) - Date.parse(b.at));
}

function formatAt(iso: string): string {
  const t = Date.parse(iso);
  if (Number.isNaN(t) || t <= 0) return iso || "—";
  return new Date(t).toLocaleString();
}

const COLUMNS: Column<CorrelatedSignal>[] = [
  { key: "at", label: "Time", render: (value) => formatAt(String(value ?? "")) },
  {
    key: "bucketTitle",
    label: "Bucket",
    render: (_value, row) => (
      <Link to={bucketPath(row.bucketId)} className={`text-accent hover:underline ${focusRing}`}>
        {row.bucketTitle}
      </Link>
    ),
  },
  { key: "kind", label: "Kind" },
  { key: "text", label: "Text" },
];

export function CorrView({ buckets }: { buckets: Snapshot[] }) {
  const { id = "" } = useParams();
  const signals = findCorrelatedSignals(buckets, id);

  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <Link to={overviewPath} className={`text-sm text-fg-dim hover:text-fg ${focusRing}`}>
          <span aria-hidden="true">←</span> Overview
        </Link>
        <span className="font-mono text-xs text-fg-faint">corr {id}</span>
      </div>
      <DataTable
        columns={COLUMNS}
        rows={signals}
        empty={`no signals found for correlation id "${id}"`}
      />
    </div>
  );
}
