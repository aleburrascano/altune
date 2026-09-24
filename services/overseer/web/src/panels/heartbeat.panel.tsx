import { useContext, useEffect, useState } from "react";
import type { PanelProps, Range, SeriesPoint } from "../types";
import { fetchSeries, TokensContext } from "../api";
import { TimeSeries } from "../charts/TimeSeries";
import { Metric, Notice, Panel, Section, SignalList, StatGrid, type Signal } from "../ui";
import { formatUpdated } from "./GenericPanel";

export interface Tick {
  at: string;
  kind: string;
  text: string;
  corrId?: string;
}

export interface Data {
  ticks: Tick[];
}

const SERIES = "tick_gap_ms";
const SERIES_REFRESH_MS = 30_000;

type SeriesState =
  | { phase: "idle" }
  | { phase: "ready"; points: SeriesPoint[] }
  | { phase: "unavailable" };

function useTickGap(id: string, range: Range): SeriesState {
  const tokens = useContext(TokensContext);
  const [state, setState] = useState<SeriesState>({ phase: "idle" });

  useEffect(() => {
    if (!tokens) return;
    let active = true;
    const load = () =>
      fetchSeries(tokens, id, range).then(
        (res) => {
          if (active) setState({ phase: "ready", points: res.series[SERIES] ?? [] });
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

function formatGap(v: number): string {
  return `${Math.round(v)} ms`;
}

export default function HeartbeatPanel({ snapshot, range }: PanelProps<Data>) {
  const gap = useTickGap(snapshot.id, range);
  const ticks = snapshot.data.ticks ?? [];
  const lastTick = ticks[ticks.length - 1];
  const recent: Signal[] = [...ticks].reverse();

  return (
    <Panel title={snapshot.title} snapshot={snapshot}>
      <StatGrid>
        <Metric label="last tick" value={lastTick ? formatUpdated(lastTick.at) : "—"} />
        <Metric label="ticks retained" value={ticks.length} />
      </StatGrid>

      <Section title="Tick gap">
        {gap.phase === "unavailable" ? (
          <Notice kind="empty">tick-gap history unavailable — chart returns once it can be read.</Notice>
        ) : (
          <TimeSeries
            title="Tick gap"
            kind="line"
            colorToken="--color-accent"
            points={gap.phase === "ready" ? gap.points : []}
            formatValue={formatGap}
          />
        )}
      </Section>

      <Section title="Recent ticks">
        <SignalList signals={recent} empty="no ticks yet" />
      </Section>
    </Panel>
  );
}
