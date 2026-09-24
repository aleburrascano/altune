import { useId, useMemo } from "react";
import { Navigate, Route, Routes } from "react-router-dom";
import type { Snapshot } from "../types";
import { Overview } from "./Overview";
import { BucketDetail } from "./BucketDetail";
import { overviewPath } from "../routes";
import { useConnection } from "../hooks/useConnection";
import { Notice } from "../ui";

function LastFrameTime({ atMs }: { atMs: number }) {
  const at = new Date(atMs);
  return (
    <time dateTime={at.toISOString()} className="font-mono">
      {at.toLocaleTimeString()}
    </time>
  );
}

export function AppRoutes({ buckets }: { buckets: Snapshot[] }) {
  const byId = useMemo(() => Object.fromEntries(buckets.map((s) => [s.id, s])), [buckets]);
  const connection = useConnection();
  const stalledNoticeId = useId();
  const stalledSince = connection.conn === "stalled" ? connection.lastFrameAt : null;
  const isStalled = stalledSince !== null;

  return (
    <div className="flex flex-col gap-3">
      {isStalled ? (
        <div id={stalledNoticeId}>
          <Notice kind="stale">
            Live updates stalled. Showing data last received at <LastFrameTime atMs={stalledSince} />.
          </Notice>
        </div>
      ) : null}
      <div
        aria-describedby={isStalled ? stalledNoticeId : undefined}
        className={`transition-opacity motion-reduce:transition-none ${isStalled ? "opacity-50" : "opacity-100"}`}
      >
        <Routes>
          <Route path={overviewPath} element={<Overview snapshots={buckets} conn={connection.conn} />} />
          <Route path="/bucket/:id" element={<BucketDetail snapshots={byId} />} />
          <Route path="*" element={<Navigate to={overviewPath} replace />} />
        </Routes>
      </div>
    </div>
  );
}
