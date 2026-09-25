import type { PanelProps } from "../types";
import { useCorrLink } from "../hooks/useCorrLink";
import { useSeries, type SeriesState } from "../hooks/useSeries";
import { BarSeries, type BarSeriesItem } from "../charts/BarSeries";
import { Metric, Notice, Panel, Section, StatGrid } from "../ui";
import { SearchBox, ToggleChips } from "../ui/FilterBar";
import { useDebounced, useUrlParam } from "../hooks/useUrlParam";
import { LogTail, type LogRow } from "./LogTail";

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

const LEVELS: Level[] = ["ERROR", "WARN", "INFO", "DEBUG"];
const SEARCH_DEBOUNCE_MS = 200;

function matches(rec: LogRecord, query: string, levels: string[]): boolean {
  if (!levels.includes(rec.level.toUpperCase())) return false;
  if (query === "") return true;
  return [rec.msg, ...Object.values(rec.attrs ?? {})].some((text) => text.toLowerCase().includes(query));
}

function parseLevels(raw: string): string[] {
  const picked = raw.split(",").filter((l) => LEVELS.includes(l as Level));
  return picked.length > 0 ? picked : LEVELS;
}

function toggled(active: string[], level: string): string[] {
  const next = active.includes(level) ? active.filter((l) => l !== level) : [...active, level];
  return next.length === 0 ? LEVELS : next;
}

function LogFilters({ records }: { records: LogRecord[] }) {
  const onCorrId = useCorrLink();
  const [rawQuery, setQuery] = useUrlParam("q");
  const [rawLevels, setLevels] = useUrlParam("level");
  const query = useDebounced(rawQuery, SEARCH_DEBOUNCE_MS).trim().toLowerCase();
  const levels = parseLevels(rawLevels);
  const rows = [...records].reverse().filter((r) => matches(r, query, levels)).map(toRow);
  const filtered = query !== "" || levels.length < LEVELS.length;
  const onToggle = (level: string) => {
    const next = toggled(levels, level);
    setLevels(next.length === LEVELS.length ? "" : next.join(","));
  };
  return (
    <>
      <div className="mb-2 flex flex-wrap items-center gap-2">
        <SearchBox label="Search logs" value={rawQuery} onChange={setQuery} />
        <ToggleChips label="Levels" options={LEVELS} active={levels} onToggle={onToggle} />
      </div>
      <LogTail
        rows={rows}
        empty={filtered ? "no logs match the filter" : "no logs yet"}
        onCorrId={onCorrId}
      />
    </>
  );
}

function toRow(rec: LogRecord): LogRow {
  return {
    time: formatTime(rec.time),
    level: rec.level.toUpperCase(),
    msg: rec.msg,
    attrs: attrsText(rec.attrs),
    corrId: rec.attrs?.corr_id,
  };
}

function levelLabel(name: string): string {
  return name.length === 0 ? name : name[0].toUpperCase() + name.slice(1).toLowerCase();
}

function LevelsChart({ state }: { state: SeriesState }) {
  if (state.status === "idle") return null;
  if (state.status === "unavailable") {
    return <Notice kind="stale">level history unavailable — chart returns once it can be read.</Notice>;
  }
  const entries = Object.entries(state.series);
  if (entries.length === 0) return null;
  const items: BarSeriesItem[] = entries.map(([name, points]) => ({ name: levelLabel(name), points: points ?? [] }));
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

export default function LogsPanel({ snapshot, range }: PanelProps<Data>) {
  const data = snapshot.data;
  const records = data.records ?? [];
  const down = snapshot.state === "source_down";
  const errors = count(records, "ERROR");
  const warns = count(records, "WARN");
  const dropped = data.dropped ?? 0;
  const series = useSeries(snapshot.id, range);

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

      <div className={down ? "opacity-60" : undefined}>
        <Section title="Log tail">
          <LogFilters records={records} />
        </Section>
      </div>
    </Panel>
  );
}
