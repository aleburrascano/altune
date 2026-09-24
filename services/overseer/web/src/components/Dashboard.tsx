import { useEffect, useMemo, useState } from "react";
import { NavLink, Route, Routes, Navigate } from "react-router-dom";
import type { SupabaseClient } from "@supabase/supabase-js";
import type { Severity, Snapshot, State } from "../types";
import {
  fetchBuckets,
  openStream,
  ForbiddenError,
  UnauthorizedError,
  TokensContext,
  type TokenProvider,
} from "../api";
import { accessToken, refreshedToken } from "../auth";
import { Overview } from "./Overview";
import { BucketDetail } from "./BucketDetail";
import { bucketPath, overviewPath } from "../routes";

type Conn = "connecting" | "live" | "error";

const SEVERITY_RANK: Record<Severity, number> = { critical: 0, warn: 1, ok: 2 };
const FRESHNESS_RANK: Record<State, number> = { source_down: 0, stale: 1, live: 2 };

// worstFirst orders buckets so the one that most needs attention floats to the top
// of both the overview grid and the nav list: health severity first (critical over
// warn over ok), then freshness within a tie (source_down/stale over live), then
// title for a stable A–Z among equals.
export function worstFirst(a: Snapshot, b: Snapshot): number {
  return (
    SEVERITY_RANK[a.severity] - SEVERITY_RANK[b.severity] ||
    FRESHNESS_RANK[a.state] - FRESHNESS_RANK[b.state] ||
    a.title.localeCompare(b.title)
  );
}

// Dashboard is the design-system shell: a nav frame plus the routed content. It
// loads the initial snapshots, then subscribes to the SSE live stream, merging
// updates by bucket id, and owns that live state for both routed views. The app
// lands on the Overview (all buckets glanceable); clicking one drills into its full
// panel, with a way back. Token expiry never blanks it — the API and stream refresh
// on 401. A non-owner token surfaces a distinct "not the owner" screen (403).
export function Dashboard({
  supabase,
  onSignOut,
  ownerEmail,
}: {
  supabase: SupabaseClient;
  onSignOut: () => void;
  ownerEmail: string;
}) {
  const [snapshots, setSnapshots] = useState<Record<string, Snapshot>>({});
  const [conn, setConn] = useState<Conn>("connecting");
  const [forbidden, setForbidden] = useState(false);

  const tokens = useMemo<TokenProvider>(
    () => ({
      get: () => accessToken(supabase),
      refresh: () => refreshedToken(supabase),
    }),
    [supabase],
  );

  useEffect(() => {
    const controller = new AbortController();
    let active = true;

    function merge(snap: Snapshot) {
      if (!active) return;
      setSnapshots((prev) => ({ ...prev, [snap.id]: snap }));
    }

    (async () => {
      try {
        const initial = await fetchBuckets(tokens);
        if (!active) return;
        setSnapshots(Object.fromEntries(initial.map((s) => [s.id, s])));
        setConn("live");
      } catch (err) {
        if (handleAuthError(err)) return;
        setConn("error");
      }
      openStream(tokens, {
        onSnapshot: merge,
        onError: (err) => {
          if (handleAuthError(err)) return;
          if (active) setConn("error");
        },
      }, controller.signal);
    })();

    function handleAuthError(err: unknown): boolean {
      if (err instanceof ForbiddenError) {
        if (active) setForbidden(true);
        return true;
      }
      if (err instanceof UnauthorizedError) {
        onSignOut();
        return true;
      }
      return false;
    }

    return () => {
      active = false;
      controller.abort();
    };
  }, [tokens, onSignOut]);

  const ordered = useMemo(
    () => Object.values(snapshots).sort(worstFirst),
    [snapshots],
  );

  if (forbidden) {
    return (
      <div className="login-screen">
        <div className="login-card">
          <div className="brand">
            <span className="brand-mark">◆</span>
            <span className="brand-name">Overseer</span>
          </div>
          <p className="login-error">This account is not the owner. Access denied.</p>
          <button type="button" onClick={onSignOut}>Sign out</button>
        </div>
      </div>
    );
  }

  return (
    <div className="app-shell">
      <nav className="nav">
        <NavLink to={overviewPath} className="brand" end>
          <span className="brand-mark">◆</span>
          <span className="brand-name">Overseer</span>
        </NavLink>
        <ul className="nav-list">
          <li>
            <NavLink to={overviewPath} end className={({ isActive }) => (isActive ? "active" : "")}>
              <span className="nav-glyph" aria-hidden="true">▦</span>
              Overview
            </NavLink>
          </li>
          {ordered.map((s) => (
            <li key={s.id}>
              <NavLink
                to={bucketPath(s.id)}
                className={({ isActive }) => (isActive ? "active" : "")}
              >
                <span className={`dot dot-sev-${s.severity}`} />
                {s.title}
              </NavLink>
            </li>
          ))}
        </ul>
        <div className="nav-foot">
          <span className={`conn conn-${conn}`}>{conn}</span>
          <span className="owner">{ownerEmail}</span>
          <button type="button" className="link" onClick={onSignOut}>Sign out</button>
        </div>
      </nav>

      <main className="content">
        <TokensContext.Provider value={tokens}>
          <Routes>
            <Route path={overviewPath} element={<Overview snapshots={ordered} conn={conn} />} />
            <Route path="/bucket/:id" element={<BucketDetail snapshots={snapshots} />} />
            <Route path="*" element={<Navigate to={overviewPath} replace />} />
          </Routes>
        </TokensContext.Provider>
      </main>
    </div>
  );
}
