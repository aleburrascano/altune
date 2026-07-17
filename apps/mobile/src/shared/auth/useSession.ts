/**
 * useSession — subscribes to Supabase's auth-state stream and exposes a
 * discriminated-union session state per .claude/rules/typescript-frontend.md.
 *
 * Consumers (root _layout, screens) branch on `status` without checking
 * nullable fields. The hook starts in `loading`, transitions to `signed-in`
 * or `signed-out` once the SDK reports its current state at mount, and
 * updates on every subsequent onAuthStateChange event.
 *
 * Per ADR-0006: the SDK auto-refreshes access tokens; on refresh failure the
 * SDK emits SIGNED_OUT which flips this hook to `signed-out` — the root
 * layout's redirect then routes to /sign-in (Slice 10).
 *
 * AIDEV-WARNING: this hook owns the cache boundary for SDK-initiated identity
 * changes. `queryClient.clear()` in shared/auth/useSignOut covers only the
 * explicit Settings sign-out; an SDK-emitted SIGNED_OUT (refresh failure) or a
 * setSession user switch bypasses it entirely, leaving user A's cached library
 * readable by user B — the exact leak useSignOut's docstring exists to prevent.
 * Clear on identity CHANGE, never on every event: TOKEN_REFRESHED fires with
 * the same user and must not nuke the cache.
 *
 * Promoted from features/auth/hooks/ to shared/auth/ because 2+ features
 * consume it (auth/AuthGate, settings).
 */
import { useEffect, useRef, useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import type { Session } from '@supabase/supabase-js';

import { clearSessionExpired } from './sessionExpired';
import { supabase } from './supabaseClient';

/**
 * `useSession`'s exposed state, modeled as a discriminated union per
 * `.claude/rules/typescript-frontend.md`. Components branch on `status`
 * without checking nullable fields.
 */
export type SessionState =
  | { status: 'loading' }
  | { status: 'signed-in'; session: Session }
  | { status: 'signed-out' };

export function useSession(): SessionState {
  const [state, setState] = useState<SessionState>({ status: 'loading' });
  const queryClient = useQueryClient();
  // null is a real identity ("signed out"), so track "seen anything yet"
  // separately rather than overloading null as the seed value.
  const seededRef = useRef(false);
  const userIdRef = useRef<string | null>(null);

  useEffect(() => {
    let active = true;

    function apply(session: Session | null): void {
      if (!active) return;
      const userId = session?.user.id ?? null;
      if (seededRef.current && userIdRef.current !== userId) {
        queryClient.clear();
        clearSessionExpired();
      }
      seededRef.current = true;
      userIdRef.current = userId;
      setState(session ? { status: 'signed-in', session } : { status: 'signed-out' });
    }

    // Initial state — getSession returns the SDK's currently-known session
    // (restored from storage by the SDK at construction).
    void supabase.auth.getSession().then(({ data }) => {
      apply(data.session);
    }).catch(() => {
      // Defensive: getSession resolves with {session: null, error} rather than
      // rejecting, but a storage-layer throw would surface here.
      apply(null);
    });

    // Subsequent updates — any SIGNED_IN / SIGNED_OUT / TOKEN_REFRESHED event.
    const {
      data: { subscription },
    } = supabase.auth.onAuthStateChange((_event, session) => {
      apply(session);
    });

    return () => {
      active = false;
      subscription.unsubscribe();
    };
  }, [queryClient]);

  return state;
}
