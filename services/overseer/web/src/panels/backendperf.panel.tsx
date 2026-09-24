import { useContext, useEffect, useState } from "react";
import type { PanelProps, Range, Severity, SeriesPoint } from "../types";
import { fetchSeries, TokensContext } from "../api";
import { MultiTimeSeries } from "../charts/MultiTimeSeries";
import { TimeSeries } from "../charts/TimeSeries";
import { Sparkline, type SparklineTone } from "../charts/Sparkline";
import { DataTable, Metric, Notice, Panel, Section, SignalList, StatGrid, type Column } from "../ui";

interface Percentile {
  ms: number;
  overflow: boolean;
}

interface RouteStat {
  route: string;
  count: number;
  error_rate: number;
  error_samples: number;
  p50: Percentile;
  p95: Percentile;
  p99: Percentile;
}

interface Signal {
  at: string;
  kind: string;
  text: string;
}

export interface Data {
  routes: RouteStat[];
  throughput: Signal[];
}

const AMBER_MS = 100;
const RED_MS = 500;

function severityForMs(ms: number): Severity {
  if (ms >= RED_MS) return "critical";
  if (ms >= AMBER_MS) return "warn";
  return "ok";
}

const AMBER_ERROR_RATE = 0.01;
const RED_ERROR_RATE = 0.05;
const MIN_ERROR_SAMPLES = 10;

function isProvisionalRate(route: RouteStat): boolean {
  return (route.error_samples ?? 0) < MIN_ERROR_SAMPLES;
}

function severityForRate(route: RouteStat): Severity {
  if (route.error_rate >= RED_ERROR_RATE) return "critical";
  if (route.error_rate >= AMBER_ERROR_RATE) return "warn";
  return "ok";
}

function formatErrorRate(route: RouteStat): string {
  const pct = `${(route.error_rate * 100).toFixed(1)}%`;
  return isProvisionalRate(route) ? `${pct}?` : pct;
}

function errorRateHint(route: RouteStat): string | undefined {
  if (!isProvisionalRate(route)) return undefined;
  return `Provisional — ${formatCount(route.error_samples ?? 0)} classified response(s) this window, under the ${MIN_ERROR_SAMPLES} needed to grade.`;
}

function worstErrorRoute(routes: RouteStat[]): RouteStat | undefined {
  const graded = routes.filter((r) => !isProvisionalRate(r));
  return highestErrorRate(graded.length > 0 ? graded : routes);
}

function highestErrorRate(routes: RouteStat[]): RouteStat | undefined {
  return routes.reduce<RouteStat | undefined>(
    (worst, r) => (worst === undefined || r.error_rate > worst.error_rate ? r : worst),
    undefined,
  );
}

function formatMs(p: Percentile): string {
  const v = p.ms;
  const digits = v >= 100 ? 0 : v >= 10 ? 1 : 2;
  return `${p.overflow ? "≥" : ""}${v.toFixed(digits)} ms`;
}

function formatCount(n: number): string {
  return new Intl.NumberFormat().format(n);
}

function formatRps(v: number): string {
  return `${v.toFixed(1)} req/s`;
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
          if (active)
            setState((prev) => (prev.phase === "ready" || prev.phase === "unavailable" ? prev : { phase: "unavailable" }));
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

function SeriesCharts({ state, range }: { state: SeriesState; range: Range }) {
  if (state.phase === "unavailable") {
    return <Notice kind="down">latency and throughput history unavailable — charts return once it can be read.</Notice>;
  }
  const series = state.phase === "ready" ? state.series : {};
  return (
    <div className="flex min-w-0 flex-col gap-4">
      <MultiTimeSeries
        range={range}
        series={[
          { name: "p50", points: series.p50_ms ?? [], unit: "ms" },
          { name: "p95", points: series.p95_ms ?? [], unit: "ms" },
          { name: "p99", points: series.p99_ms ?? [], unit: "ms" },
        ]}
      />
      <TimeSeries
        title="Throughput"
        kind="area"
        colorToken="--color-accent"
        points={series.throughput_rps ?? []}
        formatValue={formatRps}
      />
    </div>
  );
}

const SEVERITY_TEXT: Record<Severity, string> = { ok: "text-ok", warn: "text-warn", critical: "text-critical" };

function severityTextClass(severity: Severity): string {
  return SEVERITY_TEXT[severity];
}

function sparklineTone(severity: Severity): SparklineTone {
  return severity;
}

interface RouteRow {
  route: string;
  count: number;
  p50: number;
  p95: number;
  p99: number;
  error_rate: number;
  trend: null;
}

function routeRow(r: RouteStat): RouteRow {
  return {
    route: r.route,
    count: r.count,
    p50: r.p50.ms,
    p95: r.p95.ms,
    p99: r.p99.ms,
    error_rate: r.error_rate,
    trend: null,
  };
}

function RouteTable({
  routes,
  seriesByKey,
  down,
}: {
  routes: RouteStat[];
  seriesByKey: Record<string, SeriesPoint[]>;
  down: boolean;
}) {
  const byRoute = new Map(routes.map((r) => [r.route, r]));
  const rows = routes.map(routeRow);

  const columns: Column<RouteRow>[] = [
    {
      key: "route",
      label: "route",
      sortable: true,
      render: (value) => (
        <span className="block max-w-[220px] truncate font-mono" title={String(value)}>
          {value}
        </span>
      ),
    },
    {
      key: "count",
      label: "reqs",
      align: "right",
      sortable: true,
      render: (value) => <span className="text-fg-dim">{formatCount(value as number)}</span>,
    },
    {
      key: "p50",
      label: "p50",
      align: "right",
      sortable: true,
      render: (_value, row) => formatMs(byRoute.get(row.route)!.p50),
    },
    {
      key: "p95",
      label: "p95",
      align: "right",
      sortable: true,
      render: (_value, row) => {
        const stat = byRoute.get(row.route)!;
        return <span className={severityTextClass(severityForMs(stat.p95.ms))}>{formatMs(stat.p95)}</span>;
      },
    },
    {
      key: "p99",
      label: "p99",
      align: "right",
      sortable: true,
      render: (_value, row) => {
        const stat = byRoute.get(row.route)!;
        return <span className={severityTextClass(severityForMs(stat.p99.ms))}>{formatMs(stat.p99)}</span>;
      },
    },
    {
      key: "error_rate",
      label: "5xx",
      align: "right",
      sortable: true,
      render: (_value, row) => {
        const stat = byRoute.get(row.route)!;
        const tone = isProvisionalRate(stat) ? "text-fg-faint" : severityTextClass(severityForRate(stat));
        return (
          <span className={tone} title={errorRateHint(stat)}>
            {formatErrorRate(stat)}
          </span>
        );
      },
    },
    {
      key: "trend",
      label: "p95 trend",
      align: "right",
      render: (_value, row) => {
        const stat = byRoute.get(row.route)!;
        return (
          <div className="w-24">
            <Sparkline
              points={seriesByKey[`p95_ms:${row.route}`] ?? []}
              tone={sparklineTone(severityForMs(stat.p95.ms))}
              height={20}
            />
          </div>
        );
      },
    },
  ];

  return (
    <div className={`max-h-80 overflow-y-auto ${down ? "opacity-60" : ""}`}>
      <DataTable columns={columns} rows={rows} empty="no route latency yet" />
    </div>
  );
}

type BackendPerfPanelProps = { snapshot: PanelProps<Data>["snapshot"]; range?: Range };

export default function BackendPerfPanel({ snapshot, range = "1h" }: BackendPerfPanelProps) {
  const data = snapshot.data;
  const routes = data.routes ?? [];
  const throughput = data.throughput ?? [];
  const down = snapshot.state === "source_down";
  const stale = snapshot.state === "stale";
  const seriesState = useSeries(snapshot.id, range);

  const totalRequests = routes.reduce((sum, r) => sum + (r.count ?? 0), 0);
  const slowest = routes.length > 0 ? routes[0] : undefined;
  const worstError = worstErrorRoute(routes);
  const hasProvisionalRate = routes.some(isProvisionalRate);
  const worstErrorGraded = worstError && !isProvisionalRate(worstError);

  return (
    <Panel title={snapshot.title} snapshot={snapshot}>
      <p className="m-0 text-2xs uppercase tracking-wider text-fg-faint">latency and traffic reflect the recent window</p>

      {down && <Notice kind="down">go-api unreachable — showing last-known latency.</Notice>}
      {!down && stale && <Notice kind="stale">latency read is stale — showing last-known values.</Notice>}

      <StatGrid>
        <Metric label="routes" value={routes.length} />
        <Metric label="requests / window" value={formatCount(totalRequests)} />
        <Metric
          label="slowest p99 (window)"
          value={slowest ? formatMs(slowest.p99) : "—"}
          tone={slowest ? severityForMs(slowest.p99.ms) : undefined}
        />
        <Metric
          label="worst 5xx rate (window)"
          value={worstError ? formatErrorRate(worstError) : "—"}
          tone={worstError ? (worstErrorGraded ? severityForRate(worstError) : "faint") : undefined}
          hint={worstError ? errorRateHint(worstError) : undefined}
        />
      </StatGrid>

      <Section title="Latency & throughput">
        <SeriesCharts state={seriesState} range={range} />
      </Section>

      <Section title="Routes">
        <RouteTable
          routes={routes}
          seriesByKey={seriesState.phase === "ready" ? seriesState.series : {}}
          down={down}
        />
      </Section>

      {hasProvisionalRate && (
        <Notice kind="lossy">
          5xx rates marked ? are provisional — fewer than {MIN_ERROR_SAMPLES} responses this window
        </Notice>
      )}

      <Section title="Recent signals">
        <SignalList signals={throughput} empty="no throughput signal yet" />
      </Section>
    </Panel>
  );
}
