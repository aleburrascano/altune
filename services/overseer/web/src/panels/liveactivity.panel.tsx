import { useContext, useEffect, useState } from "react";
import type { PanelProps, Range, SeriesPoint } from "../types";
import { fetchSeries, TokensContext } from "../api";
import { TimeSeries } from "../charts/TimeSeries";
import { Metric, Notice, Panel, REASON_LABELS, Section, SignalList, StatGrid, type Signal } from "../ui";

export interface Data {
  events: Signal[];
  inFlight: number;
  inFlightAvailable: boolean;
  dropped?: number;
}

const EVENTS_SERIES = "events";
const SERIES_REFRESH_MS = 30_000;

type SeriesState =
  | { phase: "idle" }
  | { phase: "ready"; points: SeriesPoint[] }
  | { phase: "unavailable" };

function useEventSeries(id: string, range: Range): SeriesState {
  const tokens = useContext(TokensContext);
  const [state, setState] = useState<SeriesState>({ phase: "idle" });

  useEffect(() => {
    if (!tokens) return;
    let active = true;
    const load = () =>
      fetchSeries(tokens, id, range).then(
        (res) => {
          if (active) setState({ phase: "ready", points: res.series[EVENTS_SERIES] ?? [] });
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

function formatEventCount(v: number): string {
  return `${Math.round(v)}`;
}

export default function LiveActivityPanel({
  snapshot,
  range = "1h",
}: Pick<PanelProps<Data>, "snapshot"> & Partial<Pick<PanelProps<Data>, "range">>) {
  const data = snapshot.data;
  const events = [...(data.events ?? [])].reverse();
  const dropped = data.dropped ?? 0;
  const series = useEventSeries(snapshot.id, range);

  return (
    <Panel title={snapshot.title} snapshot={snapshot}>
      {snapshot.state === "source_down" && (
        <Notice kind="down">go-api unreachable — showing last-known activity.</Notice>
      )}
      {snapshot.state === "stale" && (
        <Notice kind="stale">
          {snapshot.reason ? REASON_LABELS[snapshot.reason] : "stale"} — showing last-known activity.
        </Notice>
      )}
      {dropped > 0 && <Notice kind="lossy">{dropped} event(s) dropped — this feed is lossy.</Notice>}

      <StatGrid>
        <Metric label="events" value={events.length} />
        <Metric label="in flight" value={data.inFlightAvailable ? data.inFlight : "—"} />
      </StatGrid>

      <Section title="Event volume">
        {series.phase === "unavailable" ? (
          <Notice kind="down">event history unavailable — chart returns once it can be read.</Notice>
        ) : (
          <TimeSeries
            title="Events"
            kind="area"
            colorToken="--color-accent"
            points={series.phase === "ready" ? series.points : []}
            formatValue={formatEventCount}
          />
        )}
      </Section>

      <Section title="Recent activity">
        <SignalList signals={events} empty="no events yet" />
      </Section>
    </Panel>
  );
}
