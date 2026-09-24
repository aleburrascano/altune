import { useContext, useEffect, useState } from "react";
import type { PanelProps, Range, SeriesPoint } from "../types";
import { fetchSeries, TokensContext } from "../api";
import { BarSeries, type BarSeriesItem } from "../charts/BarSeries";
import { DataTable, Metric, Notice, Panel, Section, StatGrid, type Column } from "../ui";

interface LogRecord {
  time: string;
  level: string;
  msg: string;
  attrs?: Record<string, string>;
}

export interface Data {
  records: LogRecord[];
  minLevel: string;
  dropped?: number;
}

type Level = "ERROR" | "WARN" | "INFO" | "DEBUG";

function count(records: LogRecord[], level: Level): number {
  return records.filter((r) => r.level.toUpperCase() === level).length;
}

function attrsText(attrs?: Record<string, string>): string | null {
  const pairs = attrs ? Object.entries(attrs) : [];
  return pairs.length > 0 ? pairs.map(([k, v]) => `${k}=${v}`).join(" ") : null;
}

function formatTime(iso: string): string {
  const t = Date.parse(iso);
  if (Number.isNaN(t) || t <= 0) return iso || "—";
  return new Date(t).toLocaleTimeString();
}

interface LogRow {
  time: string;
  level: string;
  msg: string;
  attrs: string | null;
}

const LOG_COLUMNS: Column<LogRow>[] = [
  { key: "time", label: "time" },
  { key: "level", label: "level" },
  { key: "msg", label: "message" },
  { key: "attrs", label: "attrs" },
];

function toRow(rec: LogRecord): LogRow {
  return { time: formatTime(rec.time), level: rec.level.toUpperCase(), msg: rec.msg, attrs: attrsText(rec.attrs) };
}

function levelLabel(name: string): string {
  return name.length === 0 ? name : name[0].toUpperCase() + name.slice(1).toLowerCase();
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

function LevelsChart({ state }: { state: SeriesState }) {
  if (state.phase !== "ready") return null;
  const entries = Object.entries(state.series);
  if (entries.length === 0) return null;
  const items: BarSeriesItem[] = entries.map(([name, points]) => ({ name: levelLabel(name), points }));
  const shapeKey = items.map((i) => i.name).join(",");
  return (
    <Section title="Levels over time">
      <BarSeries key={shapeKey} series={items} stacked />
    </Section>
  );
}

function droppedNotice(dropped: number): string {
  const noun = dropped === 1 ? "line" : "lines";
  return `${dropped} log ${noun} dropped — the bounded tail overflowed and the oldest lines were lost.`;
}

export default function LogsPanel({
  snapshot,
  range = "1h",
}: Omit<PanelProps<Data>, "range"> & { range?: Range }) {
  const data = snapshot.data;
  const records = data.records ?? [];
  const tail = [...records].reverse();
  const down = snapshot.state === "source_down";
  const errors = count(records, "ERROR");
  const warns = count(records, "WARN");
  const dropped = data.dropped ?? 0;
  const series = useSeries(snapshot.id, range);
  const rows = tail.map(toRow);

  return (
    <Panel title={snapshot.title} snapshot={snapshot}>
      <StatGrid>
        <Metric label="lines" value={records.length} />
        <Metric label="errors" value={errors} tone={errors > 0 ? "critical" : undefined} />
        <Metric label="warnings" value={warns} tone={warns > 0 ? "warn" : undefined} />
        <Metric label="min level" value={data.minLevel || "ALL"} />
      </StatGrid>

      {down && <Notice kind="down">go-api unreachable — showing last-known logs.</Notice>}
      {snapshot.state === "stale" && <Notice kind="stale">stream stalled — showing last-known logs.</Notice>}
      {dropped > 0 && <Notice kind="lossy">{droppedNotice(dropped)}</Notice>}

      <LevelsChart state={series} />

      <Section title="Log tail">
        <DataTable columns={LOG_COLUMNS} rows={rows} empty="no logs yet" />
      </Section>
    </Panel>
  );
}
