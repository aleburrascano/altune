import { useEffect, useRef, useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import type { AuthChangeEvent, Session } from '@supabase/supabase-js';

import { isSessionFetchFailure } from '@shared/errors';
import { notifyIdentityChange } from '@shared/session/signOutCleanup';

import { forgetPreviousUsersLocalData } from './forgetPreviousUsersLocalData';
import { renewSessionCredentials } from './sessionExpired';
import { supabase } from './supabaseClient';

export type SessionState =
  | { status: 'loading' }
  | { status: 'signed-in'; session: Session }
  | { status: 'signed-out' };

function renewsTheSignedInUser(
  event: AuthChangeEvent,
  incoming: Session | null,
  signedInUserId: string | null,
): boolean {
  return (
    event === 'TOKEN_REFRESHED' && signedInUserId !== null && incoming?.user?.id === signedInUserId
  );
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
      notifyIdentityChange(userId);
      seededRef.current = true;
      userIdRef.current = userId;
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
    } = supabase.auth.onAuthStateChange((event, session) => {
      if (active && renewsTheSignedInUser(event, session, userIdRef.current)) {
        renewSessionCredentials();
      }
      apply(session);
    });

    return () => {
      active = false;
      subscription.unsubscribe();
    };
  }, [queryClient]);

  return state;
}
