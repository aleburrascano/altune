import { useContext, useEffect, useState } from "react";
import type { PanelProps, Range, SeriesPoint, State } from "../types";
import { fetchSeries, TokensContext } from "../api";
import { Panel, Section, StatGrid, Metric, DataTable, Notice, RelativeTime, type Column } from "../ui";
import { StateBadge } from "./StateBadge";
import { TimeSeries } from "../charts/TimeSeries";
import { BarSeries, type BarSeriesItem } from "../charts/BarSeries";

interface SpendLine {
  service: string;
  amount: number;
}

interface Spend {
  amount: number;
  currency: string;
  periodStart: string;
  periodEnd: string;
  lines: SpendLine[] | null;
}

interface ProviderOutcomes {
  ok: number;
  quota: number;
  error: number;
}

type ProviderUsage = Record<string, ProviderOutcomes>;

export interface Data {
  spend: Spend | null;
  spendStale: boolean;
  spendUpdatedAt?: string;
  usage: ProviderUsage | null;
  usageStale: boolean;
}

function halfState(stale: boolean, hasData: boolean): State {
  if (!stale) return "live";
  return hasData ? "stale" : "source_down";
}

function formatMoney(amount: number, currency: string): string {
  const code = currency.trim();
  if (code) {
    try {
      return new Intl.NumberFormat(undefined, {
        style: "currency",
        currency: code,
        maximumFractionDigits: 2,
      }).format(amount);
    } catch {
      return `${amount.toFixed(2)} ${code}`;
    }
  }
  return amount.toFixed(2);
}

const sumOutcomes = (o: ProviderOutcomes): number => o.ok + o.quota + o.error;

const SERIES_SPEND_DAILY = "spend_daily";
const SERIES_SPEND_MONTH_TO_DATE = "spend_month_to_date";
const PROVIDER_CALLS_STEM = "provider_calls:";
const SERIES_REFRESH_MS = 30_000;

type SeriesState =
  | { phase: "idle" }
  | { phase: "ready"; series: Record<string, SeriesPoint[]> }
  | { phase: "unavailable" };

function useCostSeries(id: string, range: Range): SeriesState {
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

interface SpendRow {
  service: string;
  amount: number;
}

const SPEND_COLUMNS: Column<SpendRow>[] = [
  { key: "service", label: "Service" },
  { key: "amount", label: "Amount", align: "right" },
];

interface ProviderRow {
  provider: string;
  ok: number;
  quota: number;
  error: number;
  total: number;
}

const PROVIDER_COLUMNS: Column<ProviderRow>[] = [
  { key: "provider", label: "Provider" },
  { key: "ok", label: "OK", align: "right", sortable: true },
  { key: "quota", label: "Quota", align: "right", sortable: true },
  { key: "error", label: "Error", align: "right", sortable: true },
  { key: "total", label: "Total", align: "right", sortable: true },
];

function CostCharts({
  state,
  spendCurrency,
}: {
  state: SeriesState;
  spendCurrency: string;
}) {
  if (state.phase === "idle") return null;
  if (state.phase === "unavailable") {
    return <Notice kind="down">cost history unavailable — charts return once it can be read.</Notice>;
  }
  const spendTrend = state.series[SERIES_SPEND_MONTH_TO_DATE] ?? [];
  const dailySpend = state.series[SERIES_SPEND_DAILY] ?? [];
  return (
    <div className="flex min-w-0 flex-col gap-3 md:flex-row">
      <div className="min-w-0 md:flex-1">
        <TimeSeries
          title="Month-to-date"
          kind="area"
          colorToken="--color-accent"
          points={spendTrend}
          formatValue={(v) => formatMoney(v, spendCurrency)}
        />
      </div>
      <div className="min-w-0 md:flex-1">
        <BarSeries series={[{ name: "Daily spend", points: dailySpend }]} />
      </div>
    </div>
  );
}

function ProviderCallsChart({ state, providerNames }: { state: SeriesState; providerNames: string[] }) {
  if (providerNames.length === 0) return null;
  if (state.phase === "idle") return null;
  if (state.phase === "unavailable") {
    return <Notice kind="down">provider call history unavailable — charts return once it can be read.</Notice>;
  }
  const series: BarSeriesItem[] = providerNames.map((name) => ({
    name,
    points: state.series[`${PROVIDER_CALLS_STEM}${name}`] ?? [],
  }));
  return <BarSeries key={providerNames.join(",")} series={series} stacked />;
}

export default function CostPanel({
  snapshot,
  range = "1h",
}: Pick<PanelProps<Data>, "snapshot"> & Partial<Pick<PanelProps<Data>, "range">>) {
  const data = snapshot.data;
  const spend = data.spend;
  const usage = data.usage ?? {};
  const spendLines = spend?.lines ?? [];
  const providers = Object.entries(usage);
  const providerNames = providers.map(([name]) => name);

  const spendState = halfState(data.spendStale, spend != null);
  const usageState = halfState(data.usageStale, data.usage != null);
  const callTotal = providers.reduce((n, [, o]) => n + sumOutcomes(o), 0);

  const series = useCostSeries(snapshot.id, range);

  const spendRows: SpendRow[] = spendLines.map((l) => ({ service: l.service, amount: l.amount }));
  const providerRows: ProviderRow[] = providers.map(([name, o]) => ({
    provider: name,
    ok: o.ok,
    quota: o.quota,
    error: o.error,
    total: sumOutcomes(o),
  }));

  return (
    <Panel title={snapshot.title} snapshot={snapshot}>
      <StatGrid>
        <Metric value={spend ? formatMoney(spend.amount, spend.currency) : "—"} label="infra spend" />
        <Metric value={callTotal} label="provider calls" />
        <Metric value={providers.length} label="providers" />
      </StatGrid>

      {snapshot.state === "source_down" && (
        <Notice kind="down">
          {'both sources unreachable — showing last-known cost. See the deploy doc, "OCI cost access".'}
        </Notice>
      )}

      {spendState !== "live" && (
        <Notice kind={spendState === "source_down" ? "down" : "stale"}>
          {spendState === "source_down"
            ? 'OCI infra spend is unreachable — showing last-known spend. See the deploy doc, "OCI cost access".'
            : "infra spend read is stale — showing last-known spend."}
        </Notice>
      )}

      {usageState !== "live" && (
        <Notice kind={usageState === "source_down" ? "down" : "stale"}>
          {usageState === "source_down"
            ? "provider usage is unreachable — showing last-known usage."
            : "provider usage read is stale — showing last-known usage."}
        </Notice>
      )}

      <CostCharts state={series} spendCurrency={spend?.currency ?? ""} />

      <Section title="Infra spend (OCI)">
        <div className="flex items-center justify-between gap-3">
          <StateBadge state={spendState} />
          {data.spendUpdatedAt ? (
            <span className="flex items-center gap-1 text-xs text-fg-faint">
              as of <RelativeTime at={data.spendUpdatedAt} />
            </span>
          ) : null}
        </div>
        <DataTable
          columns={SPEND_COLUMNS}
          rows={spendRows}
          empty={spend ? "no per-service breakdown" : "no spend read yet"}
        />
      </Section>

      <ProviderCallsChart state={series} providerNames={providerNames} />

      <Section title="Provider API usage">
        <div className="flex items-center justify-between gap-3">
          <StateBadge state={usageState} />
        </div>
        <DataTable columns={PROVIDER_COLUMNS} rows={providerRows} empty="no provider usage yet" />
      </Section>
    </Panel>
  );
}
