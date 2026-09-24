import { useMemo } from "react";
import { Navigate, Route, Routes } from "react-router-dom";
import type { Snapshot } from "../types";
import { Overview } from "./Overview";
import { BucketDetail } from "./BucketDetail";
import { overviewPath } from "../routes";

export type Conn = "connecting" | "live" | "error";

export function AppRoutes({ buckets, conn }: { buckets: Snapshot[]; conn: Conn }) {
  const byId = useMemo(() => Object.fromEntries(buckets.map((s) => [s.id, s])), [buckets]);
  return (
    <Routes>
      <Route path={overviewPath} element={<Overview snapshots={buckets} conn={conn} />} />
      <Route path="/bucket/:id" element={<BucketDetail snapshots={byId} />} />
      <Route path="*" element={<Navigate to={overviewPath} replace />} />
    </Routes>
  );
}
