import { useContext, useEffect, useState } from "react";
import type { Range, Snapshot, SeriesPoint } from "../types";
import { fetchSeries, TokensContext } from "../api";
import { MultiTimeSeries } from "../charts/MultiTimeSeries";
import { DataTable, Metric, Notice, Panel, Section, StatGrid, type Column } from "../ui";
import { SearchBox, ToggleChips } from "../ui/FilterBar";
import { useDebounced, useUrlParam } from "../hooks/useUrlParam";

interface Count {
  label: string;
  count: number;
}

export interface Data {
  searches: Count[];
  plays: Count[];
  timeline: Count[];
  droppedKeys?: number;
  dropped?: number;
}

const sum = (rows: Count[]): number => rows.reduce((n, r) => n + r.count, 0);
const peak = (rows: Count[]): number => rows.reduce((n, r) => Math.max(n, r.count), 0);

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

const SEARCH_COLUMNS: Column<Count>[] = [
  { key: "label", label: "Query", sortable: true },
  { key: "count", label: "Count", align: "right", sortable: true },
];

const PLAY_COLUMNS: Column<Count>[] = [
  { key: "label", label: "Kind", sortable: true },
  { key: "count", label: "Count", align: "right", sortable: true },
];

const KINDS = ["searches", "plays"];
const FILTER_DEBOUNCE_MS = 200;

function parseKinds(raw: string): string[] {
  const picked = raw.split(",").filter((k) => KINDS.includes(k));
  return picked.length > 0 ? picked : KINDS;
}

function toggledKinds(active: string[], kind: string): string[] {
  const next = active.includes(kind) ? active.filter((k) => k !== kind) : [...active, kind];
  return next.length === 0 ? KINDS : next;
}

function UsageTables({ searches, plays }: { searches: Count[]; plays: Count[] }) {
  const [rawQuery, setQuery] = useUrlParam("q");
  const [rawKinds, setKinds] = useUrlParam("kind");
  const query = useDebounced(rawQuery, FILTER_DEBOUNCE_MS).trim().toLowerCase();
  const kinds = parseKinds(rawKinds);
  const keep = (rows: Count[]) => rows.filter((r) => r.label.toLowerCase().includes(query));
  const onToggle = (kind: string) => {
    const next = toggledKinds(kinds, kind);
    setKinds(next.length === KINDS.length ? "" : next.join(","));
  };
  return (
    <>
      <div className="mb-2 flex flex-wrap items-center gap-2">
        <SearchBox label="Filter by key" value={rawQuery} onChange={setQuery} />
        <ToggleChips label="Kinds" options={KINDS} active={kinds} onToggle={onToggle} />
      </div>
      {kinds.includes("searches") && (
        <Section title="Top searches">
          <DataTable columns={SEARCH_COLUMNS} rows={keep(searches)} empty="no searches yet" />
        </Section>
      )}
      {kinds.includes("plays") && (
        <Section title="Plays by kind">
          <DataTable columns={PLAY_COLUMNS} rows={keep(plays)} empty="no plays yet" />
        </Section>
      )}
    </>
  );
}

function UsageActivity({ state, range }: { state: SeriesState; range: Range }) {
  if (state.phase === "idle") return null;
  if (state.phase === "unavailable") {
    return <Notice kind="empty">usage history unavailable — the chart returns once it can be read.</Notice>;
  }
  return (
    <MultiTimeSeries
      range={range}
      series={[
        { name: "requests/min", points: state.series.requests_per_min ?? [] },
        { name: "active users", points: state.series.active_users ?? [] },
      ]}
    />
  );
}

function droppedKeysNotice(droppedKeys: number) {
  if (droppedKeys <= 0) return null;
  const noun = droppedKeys === 1 ? "key" : "keys";
  return (
    <Notice kind="lossy">
      {droppedKeys} low-count search/play {noun} dropped from the rollup to stay bounded — the window is lossy.
    </Notice>
  );
}

export default function UsagePanel({ snapshot, range = "1h" }: { snapshot: Snapshot<Data>; range?: Range }) {
  const data = snapshot.data;
  const searches = data.searches ?? [];
  const plays = data.plays ?? [];
  const timeline = data.timeline ?? [];
  const droppedKeys = data.droppedKeys ?? 0;
  const series = useSeries(snapshot.id, range);
  const down = snapshot.state === "source_down";
  const empty = searches.length === 0 && plays.length === 0 && timeline.length === 0;

  return (
    <Panel title={snapshot.title} snapshot={snapshot}>
      {down && <Notice kind="down">go-api unreachable — showing last-known usage.</Notice>}
      {!down && snapshot.state === "stale" && (
        <Notice kind="stale">stream stalled — showing last-known usage.</Notice>
      )}
      {droppedKeysNotice(droppedKeys)}

      {empty ? (
        <Notice kind="empty">no usage yet</Notice>
      ) : (
        <>
          <StatGrid>
            <Metric label="searches" value={sum(searches)} />
            <Metric label="plays" value={sum(plays)} />
            <Metric label="peak / min" value={peak(timeline)} />
          </StatGrid>

          <Section title="Activity">
            <UsageActivity state={series} range={range} />
          </Section>

          <UsageTables searches={searches} plays={plays} />
        </>
      )}
    </Panel>
  );
}
