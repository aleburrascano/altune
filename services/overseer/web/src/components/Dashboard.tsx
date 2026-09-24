import { useEffect, useMemo, useState } from "react";
import type { SupabaseClient } from "@supabase/supabase-js";
import type { Snapshot } from "../types";
import {
  fetchBuckets,
  openStream,
  ForbiddenError,
  UnauthorizedError,
  TokensContext,
  type TokenProvider,
} from "../api";
import { accessToken, refreshedToken } from "../auth";
import { worstFirst } from "../lib/order";
import { ConnectionContext, useConnection, useConnectionTracker, type Conn } from "../hooks/useConnection";
import { Shell } from "./Shell";
import { AppRoutes } from "./AppRoutes";

const CONN_TONE: Record<Conn, string> = {
  live: "text-ok",
  connecting: "text-warn",
  stalled: "text-warn",
  error: "text-critical",
};

const CONN_DOT: Record<Conn, string> = {
  live: "bg-ok",
  connecting: "bg-warn",
  stalled: "bg-warn",
  error: "bg-critical",
};

function ConnectionPill() {
  const { conn } = useConnection();
  return (
    <span
      role="status"
      aria-label={`Connection ${conn}`}
      className={`inline-flex items-center gap-1.5 font-mono text-2xs uppercase tracking-wider ${CONN_TONE[conn]}`}
    >
      <span aria-hidden="true" className={`size-1.5 shrink-0 rounded-full ${CONN_DOT[conn]}`} />
      <span>{conn}</span>
    </span>
  );
}

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
  const { connection, markFrame, markFailed } = useConnectionTracker();
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
      markFrame();
      setSnapshots((prev) => ({ ...prev, [snap.id]: snap }));
    }

    (async () => {
      try {
        const initial = await fetchBuckets(tokens);
        if (!active) return;
        setSnapshots(Object.fromEntries(initial.map((s) => [s.id, s])));
        markFrame();
      } catch (err) {
        if (handleAuthError(err)) return;
        if (active) markFailed();
      }
      openStream(tokens, {
        onSnapshot: merge,
        onError: (err) => {
          if (handleAuthError(err)) return;
          if (active) markFailed();
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
  }, [tokens, onSignOut, markFrame, markFailed]);

  const ordered = useMemo(() => worstFirst(Object.values(snapshots)), [snapshots]);

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
    <TokensContext.Provider value={tokens}>
      <ConnectionContext.Provider value={connection}>
        <Shell buckets={ordered} status={<ConnectionPill />} user={{ email: ownerEmail }} onSignOut={onSignOut}>
          <AppRoutes buckets={ordered} />
        </Shell>
      </ConnectionContext.Provider>
    </TokensContext.Provider>
  );
}
