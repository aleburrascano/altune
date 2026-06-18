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
 */
import { useEffect, useState } from 'react';

import { supabase } from '../api/supabaseClient';
import type { SessionState } from '../types';

export function useSession(): SessionState {
  const [state, setState] = useState<SessionState>({ status: 'loading' });

  useEffect(() => {
    let active = true;

    // Initial state — getSession returns the SDK's currently-known session
    // (restored from storage by the SDK at construction).
    void supabase.auth.getSession().then(({ data }) => {
      if (!active) return;
      setState(
        data.session ? { status: 'signed-in', session: data.session } : { status: 'signed-out' },
      );
    }).catch(() => {
      // Stale or revoked refresh token — redirect to sign-in.
      if (active) setState({ status: 'signed-out' });
    });

    // Subsequent updates — any SIGNED_IN / SIGNED_OUT / TOKEN_REFRESHED event.
    const {
      data: { subscription },
    } = supabase.auth.onAuthStateChange((_event, session) => {
      if (!active) return;
      setState(session ? { status: 'signed-in', session } : { status: 'signed-out' });
    });

    return () => {
      active = false;
      subscription.unsubscribe();
    };
  }, []);

  return state;
}
