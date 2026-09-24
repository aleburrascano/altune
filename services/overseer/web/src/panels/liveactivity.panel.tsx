import type { PanelProps } from "../types";
import { useSeries } from "../hooks/useSeries";
import { TimeSeries } from "../charts/TimeSeries";
import { Metric, Notice, Panel, REASON_LABELS, Section, SignalList, StatGrid, type Signal } from "../ui";

export interface Data {
  events: Signal[];
  inFlight: number;
  inFlightAvailable: boolean;
  dropped?: number;
}

const EVENTS_SERIES = "events";
function formatEventCount(v: number): string {
  return `${Math.round(v)}`;
}

export default function LiveActivityPanel({ snapshot, range }: PanelProps<Data>) {
  const data = snapshot.data;
  const events = [...(data.events ?? [])].reverse();
  const dropped = data.dropped ?? 0;
  const series = useSeries(snapshot.id, range);

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
        {series.status === "unavailable" ? (
          <Notice kind="down">event history unavailable — chart returns once it can be read.</Notice>
        ) : (
          <TimeSeries
            title="Events"
            kind="area"
            colorToken="--color-accent"
            points={series.series[EVENTS_SERIES] ?? []}
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
