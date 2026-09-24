import { Link, useParams } from "react-router-dom";
import type { Snapshot } from "../types";
import { panelFor } from "../panels/registry";
import { overviewPath } from "../routes";

// BucketDetail is the drill-down view: the full panel for one bucket, resolved by
// id through the registry (a bespoke panel or the generic fallback), with a way back
// to the overview. It updates live because the parent feeds it the merged snapshot
// map. If the id has no snapshot yet (still loading, or an unknown deep link) it
// shows the back-link and a clean notice rather than crashing.
export function BucketDetail({ snapshots }: { snapshots: Record<string, Snapshot> }) {
  const { id = "" } = useParams();
  const snapshot = snapshots[id];
  const Panel = panelFor(id);

  return (
    <div className="detail">
      <div className="detail-bar">
        <Link to={overviewPath} className="back-link">
          <span aria-hidden="true">←</span> Overview
        </Link>
      </div>
      {snapshot ? (
        <div className="grid">
          <section id={snapshot.id} className="grid-cell">
            <Panel snapshot={snapshot} range="1h" />
          </section>
        </div>
      ) : (
        <p className="empty">No data for bucket “{id}” yet.</p>
      )}
    </div>
  );
}
