import { useContext, useEffect, useState, type CSSProperties } from "react";
import type { PanelProps, Range, SeriesPoint, State } from "../types";
import { fetchSeries, TokensContext } from "../api";
import { TimeSeries } from "../charts/TimeSeries";
import { StateBadge } from "./StateBadge";
import { formatUpdated } from "./GenericPanel";

// Bespoke panel for the reliability bucket. It mirrors the bucket's Go payload
// (internal/buckets/reliability/reliability.go) and renders a control-room view:
// the authoritative own-poll reachability + uptime, the go-api dependency pills
// (DB/Redis/Auth) mirrored from /admin/health, process gauges, a reachability
// strip, and the bounded health-sample feed.
//
// Dependency statuses and error strings are watched-app data rendered as plain
// text (React escapes them), never through dangerouslySetInnerHTML.

interface HealthDetail {
  db_latency_ms: number;
  db_error?: string;
  redis_latency_ms: number;
  redis_error?: string;
  auth_latency_ms: number;
  auth_error?: string;
  checked_at: string;
}

interface OperatorHealth {
  db: string;
  redis: string;
  auth: string;
  detail: HealthDetail;
  goroutines: number;
  heap_mb: number;
}

interface Signal {
  at: string;
  kind: string;
  text: string;
}

// Data mirrors the reliability bucket's core.Snapshot().Data payload: the
// authoritative own-poll reachability, the last-known mirrored dependency health
// (null until first mirrored), whether that mirror is stale, the bounded health
// history, and the bounded reachability-poll history (the uptime signal).
export interface Data {
  reachability: string;
  health: OperatorHealth | null;
  adminStale: boolean;
  history: Signal[];
  poll: Signal[];
}

type Dep = { name: string; status: string; latencyMs: number; error?: string };

function deps(h: OperatorHealth): Dep[] {
  return [
    { name: "Database", status: h.db, latencyMs: h.detail.db_latency_ms, error: h.detail.db_error },
    { name: "Redis", status: h.redis, latencyMs: h.detail.redis_latency_ms, error: h.detail.redis_error },
    { name: "Auth", status: h.auth, latencyMs: h.detail.auth_latency_ms, error: h.detail.auth_error },
  ];
}

// depColor maps a dependency status onto the design tokens. go-api reports "down"
// for a failed dependency (everything else is healthy), matching the bucket's own
// Healthy() verdict; an empty status is "not yet observed".
function depColor(status: string): string {
  if (status === "down") return "var(--down)";
  if (status === "") return "var(--fg-faint)";
  return "var(--live)";
}

const REACH: Record<string, { label: string; color: string; dot: State }> = {
  up: { label: "Reachable", color: "var(--live)", dot: "live" },
  down: { label: "Unreachable", color: "var(--down)", dot: "source_down" },
  connecting: { label: "Connecting", color: "var(--stale)", dot: "stale" },
};

function reachOf(v: string): { label: string; color: string; dot: State } {
  return REACH[v] ?? { label: "Unknown", color: "var(--fg-faint)", dot: "stale" };
}

// uptimePct is the share of the poller's own reachability probes that saw go-api
// up, over its bounded window — the authoritative uptime, independent of the
// admin-health mirror. Null when no probe has landed yet.
function uptimePct(poll: Signal[]): number | null {
  if (poll.length === 0) return null;
  const up = poll.filter((s) => s.text === "up").length;
  return Math.round((up / poll.length) * 1000) / 10;
}

const card: CSSProperties = {
  border: "1px solid var(--border)",
  borderRadius: 7,
  padding: "9px 10px",
  background: "var(--bg)",
};

const SERIES_REFRESH_MS = 30_000;

type SeriesState =
  | { phase: "idle" }
  | { phase: "ready"; series: Record<string, SeriesPoint[]> }
  | { phase: "unavailable" };

function useSeries(id: string, range: Range): SeriesState {
  const tokens = useContext(TokensContext);
  const [state, setState] = useState<SeriesState>({ phase: "idle" });

  useEffect(() => {
    if (!tokens) return;
    let active = true;
    const load = () =>
      fetchSeries(tokens, id, range).then(
        (res) => {
          if (active) setState({ phase: "ready", series: res.series });
        },
        () => {
          if (active) setState((prev) => (prev.phase === "ready" ? prev : { phase: "unavailable" }));
        },
      );
    void load();
    const timer = setInterval(load, SERIES_REFRESH_MS);
    return () => {
      active = false;
      clearInterval(timer);
    };
  }, [tokens, id, range]);

  return state;
}

function formatUp(v: number): string {
  if (v >= 1) return "up";
  if (v <= 0) return "down";
  return "";
}

function formatLatency(v: number): string {
  return `${Math.round(v)} ms`;
}

function ReliabilityCharts({ state }: { state: SeriesState }) {
  if (state.phase === "idle") return null;
  if (state.phase === "unavailable") {
    return <p className="notice">reachability history unavailable — charts return once it can be read.</p>;
  }
  return (
    <div className="flex min-w-0 flex-col gap-3 md:flex-row">
      <div className="min-w-0 md:flex-1">
        <TimeSeries
          title="Uptime"
          kind="area"
          colorToken="--color-ok"
          points={state.series.up ?? []}
          formatValue={formatUp}
          valueRange={[0, 1]}
        />
      </div>
      <div className="min-w-0 md:flex-1">
        <TimeSeries
          title="Latency"
          kind="line"
          colorToken="--color-accent"
          points={state.series.latency_ms ?? []}
          formatValue={formatLatency}
        />
      </div>
    </div>
  );
}

// ReliabilityPanel renders all three states: live streams fresh, stale flags the
// admin mirror, and source_down keeps the last-known health (dimmed) rather than
// blanking the panel.
export default function ReliabilityPanel({ snapshot, range }: PanelProps<Data>) {
  const series = useSeries(snapshot.id, range);
  const data = snapshot.data;
  const reach = reachOf(data.reachability);
  const poll = data.poll ?? [];
  const uptime = uptimePct(poll);
  const health = data.health;
  const history = [...(data.history ?? [])].reverse();
  const down = snapshot.state === "source_down";
  const strip = poll.slice(-40);

  return (
    <div className="panel">
      <header className="panel-head">
        <h2>{snapshot.title}</h2>
        <StateBadge state={snapshot.state} />
      </header>

      <div className="la-metrics" style={{ alignItems: "center", flexWrap: "wrap", rowGap: 12 }}>
        <div className="metric">
          <span
            className="metric-value"
            style={{ display: "flex", alignItems: "center", gap: 8, color: reach.color, fontSize: 20 }}
          >
            <span className={`dot dot-${reach.dot}`} />
            {reach.label}
          </span>
          <span className="metric-label">go-api reachability</span>
        </div>
        <div className="metric">
          <span className="metric-value">{uptime === null ? "—" : `${uptime}%`}</span>
          <span className="metric-label">uptime · {poll.length} probes</span>
        </div>
        {health && (
          <>
            <div className="metric">
              <span className="metric-value">{health.goroutines}</span>
              <span className="metric-label">goroutines</span>
            </div>
            <div className="metric">
              <span className="metric-value">
                {health.heap_mb}
                <span style={{ fontSize: 12, color: "var(--fg-faint)" }}> MB</span>
              </span>
              <span className="metric-label">heap</span>
            </div>
          </>
        )}
      </div>

      <ReliabilityCharts state={series} />

      {strip.length > 0 && (
        <div
          aria-label="recent reachability probes"
          style={{ display: "flex", gap: 2, height: 22, alignItems: "flex-end", opacity: down ? 0.55 : 1 }}
        >
          {strip.map((s, i) => (
            <span
              key={`${s.at}-${i}`}
              title={`${s.text} · ${formatUpdated(s.at)}`}
              style={{
                flex: 1,
                height: s.text === "up" ? "100%" : "45%",
                background: s.text === "up" ? "var(--live)" : "var(--down)",
                borderRadius: 2,
              }}
            />
          ))}
        </div>
      )}

      {down && <p className="notice">go-api unreachable — showing last-known health.</p>}
      {!down && data.adminStale && (
        <p className="notice">admin health read is stale — showing last-known dependency health.</p>
      )}

      {health ? (
        <div
          style={{
            display: "grid",
            gridTemplateColumns: "repeat(3, 1fr)",
            gap: 8,
            opacity: down ? 0.55 : 1,
          }}
        >
          {deps(health).map((d) => (
            <div key={d.name} style={card}>
              <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 6 }}>
                <span style={{ fontSize: 12, color: "var(--fg-dim)" }}>{d.name}</span>
                <span
                  style={{
                    fontFamily: "var(--mono)",
                    fontSize: 11,
                    textTransform: "uppercase",
                    color: depColor(d.status),
                  }}
                >
                  {d.status || "—"}
                </span>
              </div>
              <div style={{ fontFamily: "var(--mono)", fontSize: 12, color: "var(--fg)", marginTop: 4 }}>
                {d.latencyMs} ms
              </div>
              {d.error && (
                <div style={{ fontSize: 11, color: "var(--down)", marginTop: 4, wordBreak: "break-word" }}>
                  {d.error}
                </div>
              )}
            </div>
          ))}
        </div>
      ) : (
        <p className="empty">no dependency health mirrored yet</p>
      )}

      {history.length > 0 && (
        <ul className={`la-feed${down ? " dimmed" : ""}`}>
          {history.map((s, i) => (
            <li key={`${s.at}-${i}`} className="la-event">
              <span className="la-kind">{s.kind}</span>
              <span className="la-text">{s.text}</span>
              <time className="la-time">{formatUpdated(s.at)}</time>
            </li>
          ))}
        </ul>
      )}

      <footer className="panel-foot">updated {formatUpdated(snapshot.updatedAt)}</footer>
    </div>
  );
}
