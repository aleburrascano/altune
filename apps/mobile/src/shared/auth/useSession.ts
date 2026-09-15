import { useEffect, useRef, useState } from 'react';
import { useQueryClient, type QueryClient } from '@tanstack/react-query';
import type { Session } from '@supabase/supabase-js';

import { useDownloadStore } from '@shared/acquisition/downloadStore';
import { useTrackStatusStore } from '@shared/acquisition/trackStatusStore';
import { claimPinnedDownloads } from '@shared/offline/pinnedStore';
import { clearOutbox, setOutboxOwner } from '@shared/telemetry/outbox';

import { clearSessionExpired } from './sessionExpired';
import { runSignOutCleanups, setSignedInUser } from './signOutCleanup';
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
      .catch(() => {
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
