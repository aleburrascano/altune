import type { PanelProps } from "../types";
import { StateBadge } from "./StateBadge";
import { formatUpdated } from "./GenericPanel";

// LiveActivityData mirrors the liveactivity bucket's Go payload
// (internal/buckets/liveactivity). Event text is watched-app data rendered as
// plain text (React escapes it), never as HTML.
export interface LiveActivityData {
  events: { at: string; kind: string; text: string }[];
  inFlight: number;
  inFlightAvailable: boolean;
}

// LiveActivityPanel is the bespoke panel: a live feed of go-api domain events,
// newest first, with the in-flight-requests signal. It renders all three states —
// live streams, stale/source_down keep showing the last-known feed (dimmed) rather
// than going blank.
export function LiveActivityPanel({ snapshot }: PanelProps<LiveActivityData>) {
  const data = snapshot.data;
  const events = [...(data.events ?? [])].reverse();
  return (
    <div className="panel">
      <header className="panel-head">
        <h2>{snapshot.title}</h2>
        <StateBadge state={snapshot.state} />
      </header>

      <div className="la-metrics">
        <div className="metric">
          <span className="metric-value">{events.length}</span>
          <span className="metric-label">events</span>
        </div>
        <div className="metric">
          <span className="metric-value">
            {data.inFlightAvailable ? data.inFlight : "—"}
          </span>
          <span className="metric-label">in flight</span>
        </div>
      </div>

      {snapshot.state === "source_down" && (
        <p className="notice">go-api unreachable — showing last-known activity.</p>
      )}

      {events.length === 0 ? (
        <p className="empty">no events yet</p>
      ) : (
        <ul className={`la-feed${snapshot.state === "source_down" ? " dimmed" : ""}`}>
          {events.map((ev, i) => (
            <li key={`${ev.at}-${i}`} className="la-event">
              <span className="la-kind">{ev.kind}</span>
              <span className="la-text">{ev.text}</span>
              <time className="la-time">{formatUpdated(ev.at)}</time>
            </li>
          ))}
        </ul>
      )}

      <footer className="panel-foot">updated {formatUpdated(snapshot.updatedAt)}</footer>
    </div>
  );
}
