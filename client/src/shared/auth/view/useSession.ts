import { useEffect, useRef, useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';

import { isSessionFetchFailure } from '@shared/failure';
import { notifyIdentityChange } from '@shared/session';

import { authSource, type AuthSession } from '@shared/auth/ports/authSource';
import { renewSessionCredentials } from '@shared/auth/view/sessionExpired';
import { forgetPreviousUsersLocalData } from '@shared/auth/view/userLocalData';
import { warn } from '@shared/log';

export type SessionState =
  { status: 'loading' } | { status: 'signed-in'; session: AuthSession } | { status: 'signed-out' };

function renewsTheSignedInUser(
  event: string,
  incoming: AuthSession | null,
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

    function apply(incoming: AuthSession | null): void {
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

    void authSource()
      .getSession()
      .then(({ data }) => {
        apply(data.session);
      })
      .catch((error: unknown) => {
        warn('[auth] getSession failed at boot', error);
        if (isSessionFetchFailure(error)) return;
        if (!seededRef.current) apply(null);
      });

    const {
      data: { subscription },
    } = authSource().onAuthStateChange((event, session) => {
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
