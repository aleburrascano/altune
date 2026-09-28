import { useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';

import { ApiError, NetworkError, isSessionFetchFailure } from '@shared/errors';

import { withinAuthDeadline } from './authDeadline';
import { forgetPreviousUsersLocalData } from './forgetPreviousUsersLocalData';
import { clearPersistedAuthSession, supabase } from './supabaseClient';

export type SignOutResult =
  | { status: 'idle' }
  | { status: 'loading' }
  | { status: 'ok' }
  | { status: 'error'; error: unknown };

function classifySignOutFailure(error: unknown): unknown {
  if (isSessionFetchFailure(error)) {
    return new NetworkError('transport', 'sign-out could not reach the auth server');
  }
  const status = (error as { status?: unknown } | null | undefined)?.status;
  if (typeof status !== 'number' || status === 0) return error;
  return new ApiError(status, `sign-out was refused with ${status}`);
}

function signOutFailureFields(cause: unknown): { status: number } | { failure: string } {
  if (cause instanceof ApiError) return { status: cause.status };
  if (cause instanceof NetworkError) return { failure: cause.failure };
  return { failure: 'unknown' };
}

function signOutFailed(error: unknown): SignOutResult {
  const cause = classifySignOutFailure(error);
  console.warn('[auth] sign out failed', signOutFailureFields(cause));
  return { status: 'error', error: cause };
}

export function useSignOut() {
  const queryClient = useQueryClient();
  const [state, setState] = useState<SignOutResult>({ status: 'idle' });

  async function signOut(): Promise<void> {
    setState({ status: 'loading' });
    try {
      const { error } = await withinAuthDeadline(supabase.auth.signOut(), 'sign-out');
      if (error) {
        await clearPersistedAuthSession().catch(() => undefined);
        forgetPreviousUsersLocalData(queryClient);
        setState(signOutFailed(error));
        return;
      }
      forgetPreviousUsersLocalData(queryClient);
      setState({ status: 'ok' });
    } catch (error) {
      await clearPersistedAuthSession().catch(() => undefined);
      forgetPreviousUsersLocalData(queryClient);
      setState(signOutFailed(error));
    }
  }

  return { state, signOut };
}
