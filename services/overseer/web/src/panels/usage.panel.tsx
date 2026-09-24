import type { CSSProperties } from "react";
import type { PanelProps } from "../types";
import { StateBadge } from "./StateBadge";
import { formatUpdated } from "./GenericPanel";

// Panel file convention (see registry.tsx / liveactivity.panel.tsx): a bucket
// panel lives at web/src/panels/<bucketId>.panel.tsx, default-exports a
// React.FC<PanelProps<D>>, and co-locates its own payload type D here. No edit to
// registry.ts / types.ts — the registry globs this directory and keys it "usage".

// Count mirrors the Go usage.Count (internal/buckets/usage): one label -> count
// pair in a bounded rollup. Labels are watched-app data (search queries, play
// kinds, window times) rendered as plain text — React escapes them, never HTML.
interface Count {
  label: string;
  count: number;
}

// Data mirrors the Go usage.Data payload: three bounded rollups. `searches` is the
// top-N search queries (count desc), `plays` is per-kind play counts (count desc),
// and `timeline` is the activity-over-time ring, oldest window first plus the
// current in-progress window, each labelled "HH:MM".
export interface Data {
  searches: Count[];
  plays: Count[];
  timeline: Count[];
}

const sum = (rows: Count[]): number => rows.reduce((n, r) => n + r.count, 0);
const peak = (rows: Count[]): number => rows.reduce((n, r) => Math.max(n, r.count), 0);

// UsagePanel is the bespoke panel for the usage bucket: headline totals, a
// Grafana-style activity timeline, a ranked search leaderboard, and a per-kind
// play breakdown. All three states render cleanly — live shows fresh rollups;
// stale and source_down keep showing the last-known rollups (dimmed on
// source_down) with a notice, so the panel never goes blank.
export default function UsagePanel({ snapshot }: Pick<PanelProps<Data>, "snapshot">) {
  const data = snapshot.data;
  const searches = data.searches ?? [];
  const plays = data.plays ?? [];
  const timeline = data.timeline ?? [];
  const down = snapshot.state === "source_down";
  const empty = searches.length === 0 && plays.length === 0 && timeline.length === 0;

  return (
    <div className="panel">
      <header className="panel-head">
        <h2>{snapshot.title}</h2>
        <StateBadge state={snapshot.state} />
      </header>

      <div className="la-metrics">
        <Metric value={sum(searches)} label="searches" />
        <Metric value={sum(plays)} label="plays" />
        <Metric value={peak(timeline)} label="peak / min" />
      </div>

      {down && (
        <p className="notice">go-api unreachable — showing last-known usage.</p>
      )}
      {snapshot.state === "stale" && (
        <p className="notice">stream stalled — showing last-known usage.</p>
      )}

      {empty ? (
        <p className="empty">no usage yet</p>
      ) : (
        <div style={sec.body(down)}>
          <Timeline rows={timeline} />
          <Leaderboard title="Top searches" rows={searches} empty="no searches yet" />
          <Leaderboard title="Plays by kind" rows={plays} empty="no plays yet" mono />
        </div>
      )}

      <footer className="panel-foot">updated {formatUpdated(snapshot.updatedAt)}</footer>
    </div>
  );
}

function Metric({ value, label }: { value: number; label: string }) {
  return (
    <div className="metric">
      <span className="metric-value">{value}</span>
      <span className="metric-label">{label}</span>
    </div>
  );
}

// Timeline renders the activity ring as a Grafana-style bar sparkline: one bar per
// retained window, height proportional to that window's event count. Zero-count
// windows keep a hairline so gaps read as "quiet", not "missing".
function Timeline({ rows }: { rows: Count[] }) {
  if (rows.length === 0) return null;
  const max = Math.max(1, peak(rows));
  const first = rows[0]?.label ?? "";
  const last = rows[rows.length - 1]?.label ?? "";
  return (
    <section style={sec.block}>
      <SectionHead title="Activity" trailing={`${rows.length} windows`} />
      <div style={sec.chart} role="img" aria-label={`activity timeline, peak ${max} per window`}>
        {rows.map((r, i) => (
          <div
            key={`${r.label}-${i}`}
            style={sec.bar(Math.max(3, Math.round((r.count / max) * 100)))}
            title={`${r.label} — ${r.count}`}
          />
        ))}
      </div>
      <div style={sec.axis}>
        <span>{first}</span>
        <span>{last}</span>
      </div>
    </section>
  );
}

// Leaderboard renders a ranked label -> count list with a proportional fill bar
// behind each row. Labels are watched-app text rendered plainly (React-escaped).
function Leaderboard({
  title,
  rows,
  empty,
  mono,
}: {
  title: string;
  rows: Count[];
  empty: string;
  mono?: boolean;
}) {
  const max = Math.max(1, peak(rows));
  return (
    <section style={sec.block}>
      <SectionHead title={title} trailing={rows.length ? `${rows.length}` : ""} />
      {rows.length === 0 ? (
        <p className="empty" style={sec.emptyRow}>
          {empty}
        </p>
      ) : (
        <ul style={sec.list}>
          {rows.map((r, i) => (
            <li key={`${r.label}-${i}`} style={sec.row}>
              <div style={sec.fill(Math.round((r.count / max) * 100))} />
              <span style={{ ...sec.label, fontFamily: mono ? "var(--mono)" : "var(--sans)" }}>
                {r.label}
              </span>
              <span style={sec.count}>{r.count}</span>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

function SectionHead({ title, trailing }: { title: string; trailing?: string }) {
  return (
    <div style={sec.head}>
      <span style={sec.headTitle}>{title}</span>
      {trailing ? <span style={sec.headTrail}>{trailing}</span> : null}
    </div>
  );
}

// On-theme inline styles (via CSS variables from styles.css). Kept in the panel
// file so a new bucket panel stays a single-file, additive change.
const sec = {
  body: (dim: boolean): CSSProperties => ({
    display: "flex",
    flexDirection: "column",
    gap: 16,
    opacity: dim ? 0.55 : 1,
  }),
  block: { display: "flex", flexDirection: "column", gap: 8 } as CSSProperties,
  head: {
    display: "flex",
    alignItems: "baseline",
    justifyContent: "space-between",
    gap: 12,
  } as CSSProperties,
  headTitle: {
    fontSize: 11,
    textTransform: "uppercase",
    letterSpacing: "0.06em",
    color: "var(--fg-dim)",
  } as CSSProperties,
  headTrail: {
    fontSize: 11,
    fontFamily: "var(--mono)",
    color: "var(--fg-faint)",
  } as CSSProperties,
  chart: {
    display: "flex",
    alignItems: "flex-end",
    gap: 2,
    height: 64,
    padding: "0 1px",
    borderBottom: "1px solid var(--border)",
  } as CSSProperties,
  bar: (pct: number): CSSProperties => ({
    flex: 1,
    minWidth: 2,
    height: `${pct}%`,
    background: "var(--accent)",
    borderRadius: "2px 2px 0 0",
    opacity: 0.85,
  }),
  axis: {
    display: "flex",
    justifyContent: "space-between",
    fontSize: 10,
    fontFamily: "var(--mono)",
    color: "var(--fg-faint)",
  } as CSSProperties,
  list: { listStyle: "none", margin: 0, padding: 0, display: "flex", flexDirection: "column", gap: 4 } as CSSProperties,
  row: {
    position: "relative",
    display: "grid",
    gridTemplateColumns: "1fr auto",
    alignItems: "center",
    gap: 10,
    padding: "5px 8px",
    borderRadius: 6,
    overflow: "hidden",
    background: "var(--bg-elev-2)",
    fontSize: 13,
  } as CSSProperties,
  fill: (pct: number): CSSProperties => ({
    position: "absolute",
    inset: 0,
    width: `${pct}%`,
    background: "rgba(91, 140, 255, 0.14)",
    borderRight: "1px solid rgba(91, 140, 255, 0.4)",
  }),
  label: {
    position: "relative",
    color: "var(--fg)",
    overflow: "hidden",
    textOverflow: "ellipsis",
    whiteSpace: "nowrap",
  } as CSSProperties,
  count: {
    position: "relative",
    fontFamily: "var(--mono)",
    fontSize: 12,
    color: "var(--fg-dim)",
  } as CSSProperties,
  emptyRow: { margin: 0, fontSize: 13 } as CSSProperties,
};
