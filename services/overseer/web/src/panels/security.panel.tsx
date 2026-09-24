import { useContext, useEffect, useState } from "react";
import type { PanelProps, Range, SeriesPoint } from "../types";
import { fetchSeries, TokensContext } from "../api";
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
  type Signal,
} from "../ui";

interface CheckView {
  name: string;
  desc: string;
  reached: boolean;
  passed: boolean;
  status: number;
  error?: string;
}

interface HistoryEntry {
  at: string;
  kind: string;
  text: string;
}

export interface Data {
  hasRun: boolean;
  passed: number;
  total: number;
  lastRun: string;
  checks: CheckView[];
  history: HistoryEntry[];
}

type Verdict = "pass" | "fail" | "unreached";

function verdict(c: CheckView): Verdict {
  if (!c.reached) return "unreached";
  return c.passed ? "pass" : "fail";
}

const VERDICT_RANK: Record<Verdict, number> = { fail: 0, unreached: 1, pass: 2 };
const VERDICT_LABEL: Record<Verdict, string> = { pass: "✓ pass", fail: "✕ fail", unreached: "– unreached" };

interface FindingRow {
  check: string;
  desc: string;
  status: CellValue;
  result: string;
  error: CellValue;
}

const FINDING_COLUMNS: Column<FindingRow>[] = [
  { key: "check", label: "Check" },
  { key: "desc", label: "Description" },
  { key: "status", label: "Status", align: "right", sortable: true },
  { key: "result", label: "Result", sortable: true },
  { key: "error", label: "Error" },
];

function findingRows(checks: CheckView[]): FindingRow[] {
  return [...checks]
    .sort((a, b) => VERDICT_RANK[verdict(a)] - VERDICT_RANK[verdict(b)])
    .map((c) => ({
      check: c.name,
      desc: c.desc,
      status: c.reached ? c.status : null,
      result: VERDICT_LABEL[verdict(c)],
      error: c.error ?? null,
    }));
}

function dayKey(iso: string): string {
  const t = Date.parse(iso);
  if (Number.isNaN(t) || t <= 0) return "unknown";
  return new Date(t).toDateString();
}

function dayLabel(iso: string): string {
  const t = Date.parse(iso);
  if (Number.isNaN(t) || t <= 0) return "Unknown date";
  return new Date(t).toLocaleDateString(undefined, { month: "short", day: "numeric", year: "numeric" });
}

interface HistoryDay {
  key: string;
  label: string;
  signals: Signal[];
}

function groupHistoryByDay(history: HistoryEntry[]): HistoryDay[] {
  const groups: HistoryDay[] = [];
  const byKey = new Map<string, HistoryDay>();
  for (const h of [...history].reverse()) {
    const key = dayKey(h.at);
    let group = byKey.get(key);
    if (!group) {
      group = { key, label: dayLabel(h.at), signals: [] };
      byKey.set(key, group);
      groups.push(group);
    }
    group.signals.push({ at: h.at, kind: h.kind, text: h.text });
  }
  return groups;
}

const SERIES_REFRESH_MS = 30_000;

type SeriesState =
  | { phase: "idle" }
  | { phase: "ready"; series: Record<string, SeriesPoint[]> }
  | { phase: "unavailable" };

function useSecuritySeries(id: string, range: Range): SeriesState {
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

export default function SecurityPanel({ snapshot, range }: PanelProps<Data>) {
  const data = snapshot.data;
  const checks = data.checks ?? [];
  const history = data.history ?? [];
  const down = snapshot.state === "source_down";
  const stale = snapshot.state === "stale";
  const total = data.total || checks.length;
  const passed = data.passed ?? 0;
  const failing = checks.filter((c) => verdict(c) === "fail").length;
  const unreached = checks.filter((c) => verdict(c) === "unreached").length;
  const allPass = total > 0 && passed === total && failing === 0 && unreached === 0;
  const seriesState = useSecuritySeries(snapshot.id, range);
  const series: MultiSeries[] =
    seriesState.phase === "ready"
      ? [
          { name: "Open findings", points: seriesState.series.findings_open ?? [] },
          { name: "Probe failures", points: seriesState.series.probe_failures ?? [] },
        ]
      : [];
  const days = groupHistoryByDay(history);

  return (
    <Panel title={snapshot.title} snapshot={snapshot}>
      {!data.hasRun ? (
        <Notice kind="empty">no self-test run yet</Notice>
      ) : (
        <>
          {down ? <Notice kind="down">go-api unreachable — showing last-known verdict.</Notice> : null}
          {stale ? <Notice kind="stale">self-test stalled — showing last-known verdict.</Notice> : null}

          <StatGrid>
            <Metric
              label="passed"
              value={`${passed}/${total}`}
              tone={allPass ? "ok" : failing > 0 ? "critical" : "warn"}
            />
            <Metric label="failing" value={failing} tone={failing > 0 ? "critical" : "ok"} />
            <Metric label="unreached" value={unreached} tone={unreached > 0 ? "warn" : "ok"} />
          </StatGrid>

          <Section title="Trend">
            {seriesState.phase === "unavailable" ? (
              <Notice kind="lossy">finding trend unavailable — chart returns once it can be read.</Notice>
            ) : (
              <MultiTimeSeries series={series} range={range} kind="line" />
            )}
          </Section>

          <Section title="Self-tests">
            <DataTable columns={FINDING_COLUMNS} rows={findingRows(checks)} empty="no checks recorded" />
          </Section>

          {days.length === 0 ? (
            <Section title="History">
              <SignalList signals={[]} empty="no self-test runs recorded" />
            </Section>
          ) : (
            days.map((day) => (
              <Section key={day.key} title={day.label}>
                <SignalList signals={day.signals} empty="no self-test runs recorded" />
              </Section>
            ))
          )}
        </>
      )}
    </Panel>
  );
}
