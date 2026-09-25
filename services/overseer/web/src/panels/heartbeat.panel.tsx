import type { PanelProps } from "../types";
import { useCorrLink } from "../hooks/useCorrLink";
import { useSeries } from "../hooks/useSeries";
import { TimeSeries } from "../charts/TimeSeries";
import { Metric, Notice, Panel, Section, SignalList, StatGrid, type Signal } from "../ui";
import { formatUpdated } from "./GenericPanel";

export interface Data {
  ticks: Signal[];
}

const SERIES = "tick_gap_ms";
function formatGap(v: number): string {
  return `${Math.round(v)} ms`;
}

export default function HeartbeatPanel({ snapshot, range }: PanelProps<Data>) {
  const gap = useSeries(snapshot.id, range);
  const ticks = snapshot.data.ticks ?? [];
  const lastTick = ticks[ticks.length - 1];
  const recent: Signal[] = [...ticks].reverse();
  const down = snapshot.state === "source_down";
  const stale = snapshot.state === "stale";
  const onCorrId = useCorrLink();

  return (
    <Panel title={snapshot.title} snapshot={snapshot}>
      <StatGrid>
        <Metric label="last tick" value={lastTick ? formatUpdated(lastTick.at) : "—"} />
        <Metric label="ticks retained" value={ticks.length} />
      </StatGrid>

      {down && <Notice kind="down">go-api unreachable — showing last-known activity.</Notice>}
      {!down && stale && <Notice kind="stale">heartbeat is stale — showing last-known activity.</Notice>}

      <div className={down || stale ? "flex min-w-0 flex-col gap-4 opacity-55" : "flex min-w-0 flex-col gap-4"}>
        <Section title="Tick gap">
          {gap.status === "idle" ? null : gap.status === "unavailable" ? (
            <Notice kind="empty">tick-gap history unavailable — chart returns once it can be read.</Notice>
          ) : (
            <TimeSeries
              title="Tick gap"
              kind="line"
              colorToken="--color-accent"
              points={gap.series[SERIES] ?? []}
              formatValue={formatGap}
            />
          )}
        </Section>

        <Section title="Recent ticks">
          <SignalList signals={recent} empty="no ticks yet" onCorrId={onCorrId} />
        </Section>
      </div>
    </Panel>
  );
}
