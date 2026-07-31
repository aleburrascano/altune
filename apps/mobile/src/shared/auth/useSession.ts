import { useEffect, useRef, useState } from 'react';
import { useQueryClient, type QueryClient } from '@tanstack/react-query';
import type { Session } from '@supabase/supabase-js';

import { usePinnedStore } from '@shared/offline/pinnedStore';

import { clearSessionExpired } from './sessionExpired';
import { supabase } from './supabaseClient';

export type SessionState =
  | { status: 'loading' }
  | { status: 'signed-in'; session: Session }
  | { status: 'signed-out' };

function forgetPreviousUsersLocalData(queryClient: QueryClient): void {
  queryClient.clear();
  clearSessionExpired();
  usePinnedStore.getState().unpinAll();
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
      seededRef.current = true;
      userIdRef.current = userId;
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
