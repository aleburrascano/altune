import type { PanelProps, Range, Signal } from "../types";
import { useCorrLink } from "../hooks/useCorrLink";
import { useSeries, type SeriesState } from "../hooks/useSeries";
import { MultiTimeSeries, type MultiSeries } from "../charts/MultiTimeSeries";
import {
  DataTable,
  Metric,
  Notice,
  Panel,
  Section,
  SignalList,
  StatGrid,
  type CellValue,
  type Column,
} from "../ui";

export interface EvalQuery {
  query: string;
  expect: string;
  passed: boolean;
  position: number;
}

export interface EvalStatus {
  enabled: boolean;
  paused: boolean;
  state: string;
  score?: number | null;
  baseline?: number | null;
  last_run?: string | null;
  error?: string;
  queries?: EvalQuery[];
}

export interface AcquisitionStatus {
  in_flight: number;
  succeeded: number;
  failed: number;
  rejected: number;
  queue_depth: number;
  queue_capacity: number;
}

export interface DiscographyCase {
  artist: string;
  artist_ref: string;
  releases: number;
  single_provider: number;
  single_provider_no_id: number;
  provider_counts: Record<string, number> | null;
  last_seen: string;
}

export interface DiscographyQuality {
  window_days: number;
  group_by: string;
  cases: DiscographyCase[] | null;
  suspect_rate?: number;
  last_sample_at?: string;
}

export interface AcqWindow {
  rate: number;
  completed: number;
}

export interface Data {
  eval: EvalStatus | null;
  evalStale: boolean;
  evalAgeStale: boolean;
  acquisition: AcquisitionStatus | null;
  acqStale: boolean;
  acqWindow: AcqWindow | null;
  discography: DiscographyQuality | null;
  discoStale: boolean;
  discoTrend: Signal[] | null;
}

function noIDSuspectRatio(c: DiscographyCase): number | null {
  if (c.releases <= 0) return null;
  return c.single_provider_no_id / c.releases;
}

function rateableCases(cases: DiscographyCase[]): DiscographyCase[] {
  return cases.filter((c) => noIDSuspectRatio(c) !== null);
}

function pct(fraction: number): string {
  return `${Math.round(fraction * 100)}%`;
}

function sampleAge(iso: string | undefined, now: number = Date.now()): string {
  if (!iso) return "no samples";
  const t = Date.parse(iso);
  if (Number.isNaN(t) || t <= 0) return "no samples";
  const secs = Math.max(0, Math.round((now - t) / 1000));
  if (secs < 60) return `sample ${secs}s ago`;
  if (secs < 3600) return `sample ${Math.floor(secs / 60)}m ago`;
  if (secs < 86400) return `sample ${Math.floor(secs / 3600)}h ago`;
  return `sample ${Math.floor(secs / 86400)}d ago`;
}

function evalAge(iso: string | undefined | null, now: number = Date.now()): string {
  if (!iso) return "never run";
  const t = Date.parse(iso);
  if (Number.isNaN(t) || t <= 0) return "never run";
  const secs = Math.max(0, Math.round((now - t) / 1000));
  if (secs < 60) return `ran ${secs}s ago`;
  if (secs < 3600) return `ran ${Math.floor(secs / 60)}m ago`;
  if (secs < 86400) return `ran ${Math.floor(secs / 3600)}h ago`;
  return `ran ${Math.floor(secs / 86400)}d ago`;
}

function caseLabel(c: DiscographyCase): string {
  return c.artist || c.artist_ref || "unknown";
}

function toneForAcqRate(rate: number): "ok" | "warn" | "critical" {
  if (rate >= 0.9) return "ok";
  if (rate >= 0.7) return "warn";
  return "critical";
}

function toneForSuspectRate(ratio: number): "ok" | "warn" | "critical" {
  if (ratio >= 0.5) return "critical";
  if (ratio >= 0.2) return "warn";
  return "ok";
}

interface DiscoRow {
  artist: CellValue;
  suspect: CellValue;
  evidence: CellValue;
}

const DISCO_COLUMNS: Column<DiscoRow>[] = [
  { key: "artist", label: "Artist" },
  { key: "suspect", label: "No-id suspect", align: "right", sortable: true },
  { key: "evidence", label: "Evidence" },
];

function discoRows(cases: DiscographyCase[]): DiscoRow[] {
  return cases.map((c) => ({
    artist: caseLabel(c),
    suspect: pct(noIDSuspectRatio(c) ?? 0),
    evidence: `${c.single_provider}/${c.releases} single-provider, ${c.single_provider_no_id} without a shared id`,
  }));
}

function DomainQualityTrend({ state, range }: { state: SeriesState; range: Range }) {
  if (state.status === "idle") return null;
  if (state.status === "unavailable") {
    return <Notice kind="lossy">trend history unavailable — chart returns once it can be read.</Notice>;
  }
  const series: MultiSeries[] = [
    { name: "Disco success rate", points: state.series.disco_success_rate ?? [] },
    { name: "Eval score", points: state.series.eval_score ?? [] },
    { name: "Acquisition rate", points: state.series.acquisition_rate ?? [] },
  ];
  return (
    <Section title="Trend">
      {state.refetchFailed && (
        <Notice kind="stale">trend refresh failed — showing the last-known chart.</Notice>
      )}
      <MultiTimeSeries series={series} range={range} kind="line" />
    </Section>
  );
}

export default function DomainQualityPanel({ snapshot, range }: PanelProps<Data>) {
  const data = snapshot.data;
  const evalMeter = data.eval;
  const acq = data.acquisition;
  const disco = data.discography;
  const cases = rateableCases(disco?.cases ?? []);
  const trendSignals: Signal[] = [...(data.discoTrend ?? [])].reverse();
  const series = useSeries(snapshot.id, range);
  const onCorrId = useCorrLink();

  const scored = evalMeter != null && evalMeter.score != null;
  const baseline = evalMeter?.baseline ?? null;
  const scoreDelta = scored && baseline != null ? (evalMeter?.score ?? 0) - baseline : null;

  const acqWindow = data.acqWindow;
  const acqRate = acqWindow?.rate ?? null;
  const suspectRate = disco?.suspect_rate ?? null;

  const evalFreshness = `${evalAge(evalMeter?.last_run)}${
    baseline != null ? ` · baseline ${baseline.toFixed(2)}` : ""
  }${scoreDelta != null ? ` · ${scoreDelta >= 0 ? "+" : ""}${scoreDelta.toFixed(2)}` : ""}`;
  const evalHint = `${evalFreshness}${data.evalAgeStale ? " · aged" : ""}`;
  const acqHint =
    acq == null
      ? "no data"
      : acqWindow != null
        ? `${acqWindow.completed} recent · q ${acq.queue_depth}/${acq.queue_capacity}`
        : `no recent completions · q ${acq.queue_depth}/${acq.queue_capacity}`;
  const suspectHint = disco != null ? `${sampleAge(disco.last_sample_at)} · by ${disco.group_by} · ${disco.window_days}d` : sampleAge(undefined);

  const evalMeterLine = evalMeter?.state ? (
    <span className="block">
      meter {evalMeter.state}
      {evalMeter.paused && <span className="text-warn"> · paused</span>}
      {!evalMeter.enabled && <span className="text-fg-faint"> · disabled</span>}
      {evalMeter.error && <span className="text-critical"> · {evalMeter.error}</span>}
    </span>
  ) : null;

  const evalDetail = (
    <span className="flex min-w-0 flex-col gap-0.5">
      <span className={data.evalAgeStale ? "text-warn" : undefined}>
        {evalFreshness}
        {data.evalAgeStale ? " · stale" : ""}
      </span>
      {evalMeterLine}
    </span>
  );

  return (
    <Panel title={snapshot.title} snapshot={snapshot}>
      {snapshot.state === "source_down" && (
        <Notice kind="down">go-api unreachable — showing last-known domain quality.</Notice>
      )}
      {snapshot.state === "stale" && <Notice kind="stale">a source read is stale — showing last-known values.</Notice>}

      <StatGrid>
        <Metric
          label="eval score"
          value={scored ? (evalMeter?.score ?? 0).toFixed(2) : "—"}
          hint={evalHint}
          detail={evalDetail}
          tone={data.evalAgeStale ? "warn" : undefined}
        />
        <Metric
          label="acquisition"
          value={acqRate != null ? pct(acqRate) : "—"}
          tone={acqRate != null ? toneForAcqRate(acqRate) : undefined}
          hint={acqHint}
          detail={acqHint}
        />
        <Metric
          label="suspect rate"
          value={suspectRate != null ? pct(suspectRate) : "—"}
          tone={suspectRate != null ? toneForSuspectRate(suspectRate) : undefined}
          hint={suspectHint}
          detail={suspectHint}
        />
      </StatGrid>

      {data.evalStale && <Notice kind="stale">eval read is stale — showing last-known score.</Notice>}
      {data.acqStale && <Notice kind="stale">acquisition read is stale — showing last-known status.</Notice>}
      {data.discoStale && <Notice kind="stale">discography read is stale — showing last-known cases.</Notice>}

      <DomainQualityTrend state={series} range={range} />

      <Section title="Discography · worst first">
        <DataTable columns={DISCO_COLUMNS} rows={discoRows(cases.slice(0, 8))} empty="no rateable discographies" />
      </Section>

      <Section title="Discography trend">
        <SignalList signals={trendSignals} empty="no discography trend yet" onCorrId={onCorrId} />
      </Section>
    </Panel>
  );
}
