import { Link } from "react-router-dom";
import type { Snapshot } from "../types";
import { StateBadge } from "../panels/StateBadge";
import { formatUpdated } from "../panels/GenericPanel";
import { summarize } from "../summary";
import { bucketPath } from "../routes";
import { REASON_LABELS } from "../ui";
import { Sparkline } from "../charts/Sparkline";
import { HealthStrip } from "./HealthStrip";
import type { Conn } from "../hooks/useConnection";

export function Overview({
  snapshots,
  conn,
}: {
  snapshots: Snapshot[];
  conn: Conn;
}) {
  if (snapshots.length === 0) {
    return (
      <p className="empty">
        {conn === "error" ? "Overseer API unavailable." : "Loading buckets…"}
      </p>
    );
  }

  return (
    <div className="overview">
      <header className="overview-head">
        <h1>Overview</h1>
        <span className="overview-count">{snapshots.length} buckets</span>
      </header>
      <HealthStrip snapshots={snapshots} />
      <div className="overview-grid">
        {snapshots.map((snap) => {
          const summary = summarize(snap);
          const dimmed = snap.state === "source_down";
          return (
            <Link
              key={snap.id}
              to={bucketPath(snap.id)}
              className={`ov-card${dimmed ? " opacity-70" : ""}`}
              aria-label={`Open ${snap.title}`}
            >
              <div className="ov-card-head">
                <span className={`dot dot-sev-${snap.severity}`} />
                <span className="ov-title">{snap.title}</span>
                <StateBadge state={snap.state} severity={snap.severity} />
                {snap.reason ? (
                  <span className="font-mono text-2xs text-fg-dim">{REASON_LABELS[snap.reason]}</span>
                ) : null}
              </div>
              <p className="ov-summary">{summary || "—"}</p>
              {snap.spark && snap.spark.length > 0 ? (
                <Sparkline points={snap.spark} tone={snap.severity} height={28} />
              ) : null}
              <span className="ov-foot">updated {formatUpdated(snap.updatedAt)}</span>
            </Link>
          );
        })}
      </div>
    </div>
  );
}
