import { useContext, useEffect, useState } from "react";
import type { PanelProps, Range, Severity, SeriesPoint } from "../types";
import { fetchSeries, TokensContext } from "../api";
import { TimeSeries } from "../charts/TimeSeries";
import { UptimeStrip } from "../charts/UptimeStrip";
import { Panel, Section, StatGrid, Metric, DataTable, SignalList, Notice, type Column } from "../ui";

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

export interface Data {
  reachability: string;
  health: OperatorHealth | null;
  adminStale: boolean;
  history: Signal[];
  poll: Signal[];
}

interface Dep {
  name: string;
  status: string;
  latencyMs: number;
  error?: string;
}

const DEP_COLUMNS: Column<Dep>[] = [
  { key: "name", label: "Dependency" },
  { key: "status", label: "Status" },
  { key: "latencyMs", label: "Latency", align: "right" },
  { key: "error", label: "Error" },
];

function deps(h: OperatorHealth): Dep[] {
  return [
    { name: "Database", status: h.db, latencyMs: h.detail.db_latency_ms, error: h.detail.db_error },
    { name: "Redis", status: h.redis, latencyMs: h.detail.redis_latency_ms, error: h.detail.redis_error },
    { name: "Auth", status: h.auth, latencyMs: h.detail.auth_latency_ms, error: h.detail.auth_error },
  ];
}

const REACH: Record<string, { label: string; tone?: Severity }> = {
  up: { label: "Reachable", tone: "ok" },
  degraded: { label: "Degraded", tone: "warn" },
  down: { label: "Unreachable", tone: "critical" },
  connecting: { label: "Connecting" },
};

function reachOf(v: string): { label: string; tone?: Severity } {
  return REACH[v] ?? { label: "Unknown" };
}

function uptimePct(poll: Signal[]): number | null {
  if (poll.length === 0) return null;
  const up = poll.filter((s) => s.text === "up").length;
  return Math.round((up / poll.length) * 1000) / 10;
}

function pollToPoints(poll: Signal[]): SeriesPoint[] {
  return poll.map((s) => ({ at: s.at, v: s.text === "up" ? 1 : s.text === "degraded" ? 0.5 : 0 }));
}

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
    return <Notice kind="down">reachability history unavailable — charts return once it can be read.</Notice>;
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

export default function ReliabilityPanel({ snapshot, range }: PanelProps<Data>) {
  const series = useSeries(snapshot.id, range);
  const data = snapshot.data;
  const reach = reachOf(data.reachability);
  const poll = data.poll ?? [];
  const uptime = uptimePct(poll);
  const health = data.health;
  const history = [...(data.history ?? [])].reverse();
  const down = snapshot.state === "source_down";
  const depRows = health ? deps(health) : [];

  return (
    <Panel title={snapshot.title} snapshot={snapshot}>
      <StatGrid>
        <Metric label="go-api reachability" value={reach.label} tone={reach.tone} />
        <Metric label={`uptime · ${poll.length} probes`} value={uptime === null ? "—" : `${uptime}%`} />
        {health && <Metric label="goroutines" value={health.goroutines} />}
        {health && <Metric label="heap" value={health.heap_mb} unit="MB" />}
      </StatGrid>

      <ReliabilityCharts state={series} />

      <Section title="Recent probes">
        <UptimeStrip points={pollToPoints(poll.slice(-40))} />
      </Section>

      {down && <Notice kind="down">go-api unreachable — showing last-known health.</Notice>}
      {!down && data.adminStale && (
        <Notice kind="stale">admin health read is stale — showing last-known dependency health.</Notice>
      )}

      <Section title="Dependencies">
        <DataTable columns={DEP_COLUMNS} rows={depRows} empty="no dependency health mirrored yet" />
      </Section>

      <Section title="History">
        <SignalList signals={history} empty="no history yet" />
      </Section>
    </Panel>
  );
}
