import type { CSSProperties } from "react";
import type { PanelProps } from "../types";
import { StateBadge } from "./StateBadge";
import { formatUpdated } from "./GenericPanel";

// Panel file convention (see registry.tsx / liveactivity.panel.tsx): a bucket
// panel lives at web/src/panels/<bucketId>.panel.tsx, default-exports a
// React.FC<PanelProps<D>>, and co-locates its own payload type D here. No edit to
// registry.tsx / types.ts / styles.css — the registry globs this directory and
// keys this module "logs" from its filename.

// LogRecord mirrors the Go goapi.LogRecord carried in the logs bucket's payload
// (internal/buckets/logs, internal/goapi.LogRecord). Every field is watched-app
// data — the timestamp, level, message and attribute bag — rendered as plain text
// (React escapes it), never as HTML. `attrs` is omitted when empty on the wire.
interface LogRecord {
  time: string;
  level: string;
  msg: string;
  attrs?: Record<string, string>;
}

// Data mirrors the Go logs.Data payload: the level-filtered tail (oldest first)
// and the active minimum level the tail is filtered to.
export interface Data {
  records: LogRecord[];
  minLevel: string;
}

// Canonical levels the backend normalizes to (internal/buckets/logs/view.go);
// anything else is passed through and rendered with the neutral fallback.
type Level = "ERROR" | "WARN" | "INFO" | "DEBUG";

const LEVEL_COLOR: Record<Level, string> = {
  ERROR: "var(--down)",
  WARN: "var(--stale)",
  INFO: "var(--live)",
  DEBUG: "var(--fg-faint)",
};

function levelColor(level: string): string {
  return LEVEL_COLOR[level.toUpperCase() as Level] ?? "var(--fg-dim)";
}

function count(records: LogRecord[], level: Level): number {
  return records.filter((r) => r.level.toUpperCase() === level).length;
}

function attrPairs(attrs?: Record<string, string>): [string, string][] {
  return attrs ? Object.entries(attrs) : [];
}

// LogsPanel is the bespoke panel for the logs bucket: a dense, Grafana-style
// monospace log tail with per-level coloring, newest first, plus headline counts
// (total / errors / warnings) and the active minimum level. All three states
// render cleanly — live shows the fresh tail; stale and source_down keep showing
// the last-known tail (dimmed on source_down) behind a notice, so the panel never
// goes blank. Log text is watched-app data rendered as plain text; React escapes
// it — no dangerouslySetInnerHTML, ever.
export default function LogsPanel({ snapshot }: Pick<PanelProps<Data>, "snapshot">) {
  const data = snapshot.data;
  const records = data.records ?? [];
  // Newest first for quick scanning of the most recent lines (the tail arrives
  // oldest first).
  const tail = [...records].reverse();
  const down = snapshot.state === "source_down";
  const errors = count(records, "ERROR");
  const warns = count(records, "WARN");

  return (
    <div className="panel">
      <header className="panel-head">
        <h2>{snapshot.title}</h2>
        <StateBadge state={snapshot.state} />
      </header>

      <div className="la-metrics">
        <Metric value={records.length} label="lines" />
        <Metric value={errors} label="errors" color={errors ? "var(--down)" : undefined} />
        <Metric value={warns} label="warnings" color={warns ? "var(--stale)" : undefined} />
        <Metric value={data.minLevel || "ALL"} label="min level" />
      </div>

      {down && (
        <p className="notice">go-api unreachable — showing last-known logs.</p>
      )}
      {snapshot.state === "stale" && (
        <p className="notice">stream stalled — showing last-known logs.</p>
      )}

      {tail.length === 0 ? (
        <p className="empty">no logs yet</p>
      ) : (
        <ul style={sec.stream(down)}>
          {tail.map((rec, i) => {
            const pairs = attrPairs(rec.attrs);
            return (
              <li key={`${rec.time}-${i}`} style={sec.line}>
                <time style={sec.time}>{formatTime(rec.time)}</time>
                <span style={{ ...sec.level, color: levelColor(rec.level) }}>
                  {rec.level.toUpperCase()}
                </span>
                <span style={sec.msg}>
                  {rec.msg}
                  {pairs.length > 0 && (
                    <span style={sec.attrs}>
                      {pairs.map(([k, v]) => (
                        <span key={k} style={sec.attr}>
                          <span style={sec.attrKey}>{k}</span>
                          <span style={sec.attrEq}>=</span>
                          <span style={sec.attrVal}>{v}</span>
                        </span>
                      ))}
                    </span>
                  )}
                </span>
              </li>
            );
          })}
        </ul>
      )}

      <footer className="panel-foot">updated {formatUpdated(snapshot.updatedAt)}</footer>
    </div>
  );
}

function Metric({ value, label, color }: { value: number | string; label: string; color?: string }) {
  return (
    <div className="metric">
      <span className="metric-value" style={color ? { color } : undefined}>
        {value}
      </span>
      <span className="metric-label">{label}</span>
    </div>
  );
}

// formatTime renders a record's timestamp as a compact HH:MM:SS clock (the log
// tail is short-lived, so the date is noise); an unparseable value falls back to
// the raw string so a corrupt record still reads.
function formatTime(iso: string): string {
  const t = Date.parse(iso);
  if (Number.isNaN(t) || t <= 0) return iso || "—";
  return new Date(t).toLocaleTimeString();
}

// On-theme inline styles (via CSS variables from styles.css). Kept in the panel
// file so a new bucket panel stays a single-file, additive change.
const sec = {
  stream: (dim: boolean): CSSProperties => ({
    listStyle: "none",
    margin: 0,
    padding: 8,
    display: "flex",
    flexDirection: "column",
    gap: 1,
    maxHeight: 360,
    overflowY: "auto",
    background: "var(--bg)",
    border: "1px solid var(--border)",
    borderRadius: 8,
    fontFamily: "var(--mono)",
    fontSize: 12,
    lineHeight: 1.5,
    opacity: dim ? 0.55 : 1,
  }),
  line: {
    display: "grid",
    gridTemplateColumns: "auto auto 1fr",
    gap: 10,
    alignItems: "baseline",
    padding: "2px 6px",
    borderRadius: 4,
  } as CSSProperties,
  time: {
    color: "var(--fg-faint)",
    whiteSpace: "nowrap",
  } as CSSProperties,
  level: {
    fontWeight: 600,
    letterSpacing: "0.04em",
    width: "3.2em",
    flexShrink: 0,
  } as CSSProperties,
  msg: {
    color: "var(--fg)",
    wordBreak: "break-word",
    whiteSpace: "pre-wrap",
  } as CSSProperties,
  attrs: {
    display: "inline-flex",
    flexWrap: "wrap",
    gap: 8,
    marginLeft: 8,
  } as CSSProperties,
  attr: {
    display: "inline-flex",
    alignItems: "baseline",
  } as CSSProperties,
  attrKey: { color: "var(--fg-dim)" } as CSSProperties,
  attrEq: { color: "var(--fg-faint)" } as CSSProperties,
  attrVal: { color: "var(--accent)" } as CSSProperties,
};
