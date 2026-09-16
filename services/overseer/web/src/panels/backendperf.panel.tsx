import type { CSSProperties } from "react";
import type { PanelProps } from "../types";
import { StateBadge } from "./StateBadge";
import { formatUpdated } from "./GenericPanel";

// Bucket panel file convention (see registry.tsx / liveactivity.panel.tsx):
// this file default-exports a React.FC<PanelProps<D>> and co-locates its own
// payload type D — no edits to registry.ts or types.ts. The registry globs
// this directory and keys the panel by the "backendperf" in the filename.

// Percentile mirrors the Go bucket's percentile struct: one estimated latency in
// milliseconds. `overflow` is true when the estimate fell in the histogram's
// unbounded "+Inf" tail, where the value is only a lower bound.
interface Percentile {
  ms: number;
  overflow: boolean;
}

// RouteStat mirrors the Go bucket's routeStat: one route's throughput and its
// p50/p95/p99 latency estimates. `route` is a watched-app route template rendered
// as plain text (React escapes it), never as HTML.
interface RouteStat {
  route: string;
  count: number;
  p50: Percentile;
  p95: Percentile;
  p99: Percentile;
}

// Signal mirrors the Go core.Signal used for the bounded throughput trend.
interface Signal {
  at: string;
  kind: string;
  text: string;
}

// Data mirrors the backendperf bucket's Go payload
// (internal/buckets/backendperf/backendperf.go): per-route latency stats sorted
// slowest-first, and the bounded throughput trend.
export interface Data {
  routes: RouteStat[];
  throughput: Signal[];
}

// Grafana-style latency thresholds (ms): green healthy, amber warning, red hot.
const AMBER_MS = 100;
const RED_MS = 500;

function latencyColor(ms: number): string {
  if (ms >= RED_MS) return "var(--down)";
  if (ms >= AMBER_MS) return "var(--stale)";
  return "var(--live)";
}

// formatMs renders a latency estimate compactly, marking overflow (+Inf tail)
// estimates with a leading "≥" so a lower bound is never read as exact.
function formatMs(p: Percentile): string {
  const v = p.ms;
  const digits = v >= 100 ? 0 : v >= 10 ? 1 : 2;
  return `${p.overflow ? "≥" : ""}${v.toFixed(digits)} ms`;
}

function formatCount(n: number): string {
  return new Intl.NumberFormat().format(n);
}

// BackendPerfPanel is the bespoke Back-end performance panel: a Grafana-style
// per-route latency table (p50/p95/p99) with a p99 heat bar, topped by at-a-glance
// metrics. It renders all three states — on stale/source_down it keeps showing the
// last-known latency (dimmed) rather than going blank.
export default function BackendPerfPanel({ snapshot }: PanelProps<Data>) {
  const data = snapshot.data;
  const routes = data.routes ?? [];
  const throughput = data.throughput ?? [];

  const totalRequests = routes.reduce((sum, r) => sum + (r.count ?? 0), 0);
  const slowest = routes.length > 0 ? routes[0] : undefined;
  const maxP99 = routes.reduce((m, r) => Math.max(m, r.p99?.ms ?? 0), 0);
  const latest = throughput.length > 0 ? throughput[throughput.length - 1] : undefined;
  const down = snapshot.state === "source_down";

  return (
    <div className="panel">
      <header className="panel-head">
        <h2>{snapshot.title}</h2>
        <StateBadge state={snapshot.state} />
      </header>

      <div className="la-metrics">
        <div className="metric">
          <span className="metric-value">{routes.length}</span>
          <span className="metric-label">routes</span>
        </div>
        <div className="metric">
          <span className="metric-value">{formatCount(totalRequests)}</span>
          <span className="metric-label">requests</span>
        </div>
        <div className="metric">
          <span
            className="metric-value"
            style={slowest ? { color: latencyColor(slowest.p99.ms) } : undefined}
          >
            {slowest ? formatMs(slowest.p99) : "—"}
          </span>
          <span className="metric-label">slowest p99</span>
        </div>
      </div>

      {down && (
        <p className="notice">go-api unreachable — showing last-known latency.</p>
      )}

      {routes.length === 0 ? (
        <p className="empty">no route latency yet</p>
      ) : (
        <div style={dimStyle(down)}>
          <div style={rowStyle} aria-hidden="true">
            <span style={headCell}>route</span>
            <span style={numHeadCell}>p50</span>
            <span style={numHeadCell}>p95</span>
            <span style={numHeadCell}>p99</span>
            <span style={numHeadCell}>reqs</span>
          </div>
          <ul style={listStyle}>
            {routes.map((r) => (
              <li key={r.route} style={routeItemStyle}>
                <div style={rowStyle}>
                  <span style={routeCell} title={r.route}>
                    {r.route}
                  </span>
                  <span style={numCell}>{formatMs(r.p50)}</span>
                  <span style={numCell}>{formatMs(r.p95)}</span>
                  <span style={{ ...numCell, color: latencyColor(r.p99.ms) }}>
                    {formatMs(r.p99)}
                  </span>
                  <span style={{ ...numCell, color: "var(--fg-dim)" }}>
                    {formatCount(r.count)}
                  </span>
                </div>
                <div style={barTrackStyle}>
                  <div
                    style={{
                      ...barFillStyle,
                      transform: `scaleX(${maxP99 > 0 ? Math.max(0.02, r.p99.ms / maxP99) : 0})`,
                      background: latencyColor(r.p99.ms),
                    }}
                  />
                </div>
              </li>
            ))}
          </ul>
        </div>
      )}

      {latest && (
        <p style={throughputStyle}>
          <span style={{ color: "var(--accent)", fontFamily: "var(--mono)" }}>
            throughput
          </span>{" "}
          {latest.text}
        </p>
      )}

      <footer className="panel-foot">updated {formatUpdated(snapshot.updatedAt)}</footer>
    </div>
  );
}

// --- Bespoke styles (inline: this panel owns no shared CSS; tokens come from
// the design system in styles.css via var()). ---

function dimStyle(down: boolean): CSSProperties {
  return { opacity: down ? 0.55 : 1 };
}

const rowStyle: CSSProperties = {
  display: "grid",
  gridTemplateColumns: "1fr 4.5rem 4.5rem 4.5rem 4rem",
  gap: "8px",
  alignItems: "baseline",
};

const headCell: CSSProperties = {
  fontSize: "11px",
  textTransform: "uppercase",
  letterSpacing: "0.06em",
  color: "var(--fg-faint)",
};

const numHeadCell: CSSProperties = { ...headCell, textAlign: "right" };

const listStyle: CSSProperties = {
  listStyle: "none",
  margin: 0,
  padding: 0,
  display: "flex",
  flexDirection: "column",
  gap: "8px",
  maxHeight: "320px",
  overflowY: "auto",
  borderTop: "1px solid var(--border)",
  paddingTop: "8px",
  marginTop: "6px",
};

const routeItemStyle: CSSProperties = {
  display: "flex",
  flexDirection: "column",
  gap: "4px",
};

const routeCell: CSSProperties = {
  fontFamily: "var(--mono)",
  fontSize: "12px",
  color: "var(--fg)",
  overflow: "hidden",
  textOverflow: "ellipsis",
  whiteSpace: "nowrap",
};

const numCell: CSSProperties = {
  fontFamily: "var(--mono)",
  fontSize: "12px",
  textAlign: "right",
  color: "var(--fg)",
};

const barTrackStyle: CSSProperties = {
  height: "3px",
  borderRadius: "999px",
  background: "var(--bg-elev-2)",
  overflow: "hidden",
};

const barFillStyle: CSSProperties = {
  height: "100%",
  width: "100%",
  transformOrigin: "left",
  borderRadius: "999px",
  transition: "transform 0.2s ease",
};

const throughputStyle: CSSProperties = {
  margin: 0,
  fontSize: "12px",
  color: "var(--fg-dim)",
};
