import { useEffect, useRef, useState } from 'react';
import { useQueryClient, type QueryClient } from '@tanstack/react-query';
import type { Session } from '@supabase/supabase-js';

import { useDownloadStore } from '@shared/acquisition/downloadStore';
import { useTrackStatusStore } from '@shared/acquisition/trackStatusStore';
import { isSessionFetchFailure } from '@shared/api-client/errors';
import { claimPinnedDownloads } from '@shared/offline/pinnedStore';
import { runSignOutCleanups, setSignedInUser } from '@shared/session/signOutCleanup';
import { clearOutbox, setOutboxOwner } from '@shared/telemetry/outbox';

import { clearSessionExpired } from './sessionExpired';
import { supabase } from './supabaseClient';

export type SessionState =
  | { status: 'loading' }
  | { status: 'signed-in'; session: Session }
  | { status: 'signed-out' };

function forgetPreviousUsersLocalData(queryClient: QueryClient): void {
  queryClient.clear();
  clearSessionExpired();
  useDownloadStore.getState().reset();
  useTrackStatusStore.getState().reset();
  clearOutbox();
  runSignOutCleanups();
}

export function useSession(): SessionState {
  const [state, setState] = useState<SessionState>({ status: 'loading' });
  const queryClient = useQueryClient();
  const seededRef = useRef(false);
  const userIdRef = useRef<string | null>(null);

  useEffect(() => {
    let active = true;

    function apply(incoming: Session | null): void {
      if (!active) return;
      const session = incoming?.user != null ? incoming : null;
      const userId = session?.user.id ?? null;
      if (seededRef.current && userIdRef.current !== userId) {
        forgetPreviousUsersLocalData(queryClient);
      }
      setOutboxOwner(userId);
      if (userId !== null) claimPinnedDownloads(userId);
      seededRef.current = true;
      userIdRef.current = userId;
      setSignedInUser(userId !== null);
      setState(session ? { status: 'signed-in', session } : { status: 'signed-out' });
    }

    void supabase.auth
      .getSession()
      .then(({ data }) => {
        apply(data.session);
      })
      .catch((error: unknown) => {
        console.warn('[auth] getSession failed at boot', error);
        // A blip reaching the auth server is not evidence of signed-out, so the
        // same split `apiFetch`'s authorization() makes applies here: leave the
        // state unknown for the listener's INITIAL_SESSION to settle, rather than
        // bouncing a user with a valid cached session to the sign-in screen.
        if (isSessionFetchFailure(error)) return;
        if (!seededRef.current) apply(null);
      });

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
