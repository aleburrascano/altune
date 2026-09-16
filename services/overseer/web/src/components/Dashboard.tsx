import { useEffect, useMemo, useRef, useState } from "react";
import type { SupabaseClient } from "@supabase/supabase-js";
import type { Snapshot } from "../types";
import {
  fetchBuckets,
  openStream,
  ForbiddenError,
  UnauthorizedError,
  type TokenProvider,
} from "../api";
import { accessToken, refreshedToken } from "../auth";
import { panelFor } from "../panels/registry";

type Conn = "connecting" | "live" | "error";

// Dashboard is the design-system shell: a nav frame plus the themed panels. It
// loads the initial snapshots, then subscribes to the SSE live stream, merging
// updates by bucket id. Token expiry never blanks it — the API and stream refresh
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

  const selected = useRef<string | null>(null);

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
    () => Object.values(snapshots).sort((a, b) => a.id.localeCompare(b.id)),
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
        <div className="brand">
          <span className="brand-mark">◆</span>
          <span className="brand-name">Overseer</span>
        </div>
        <ul className="nav-list">
          {ordered.map((s) => (
            <li key={s.id}>
              <a
                href={`#${s.id}`}
                className={selected.current === s.id ? "active" : ""}
                onClick={() => (selected.current = s.id)}
              >
                <span className={`dot dot-${s.state}`} />
                {s.title}
              </a>
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
        {ordered.length === 0 ? (
          <p className="empty">
            {conn === "error" ? "Overseer API unavailable." : "Loading buckets…"}
          </p>
        ) : (
          <div className="grid">
            {ordered.map((snap) => {
              const Panel = panelFor(snap.id);
              return (
                <section key={snap.id} id={snap.id} className="grid-cell">
                  <Panel snapshot={snap} />
                </section>
              );
            })}
          </div>
        )}
      </main>
    </div>
  );
}
