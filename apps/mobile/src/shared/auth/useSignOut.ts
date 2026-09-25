import { useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';

import { ApiError, NetworkError, isSessionFetchFailure } from '@shared/errors';

import { withinAuthDeadline } from './authDeadline';
import { forgetPreviousUsersLocalData } from './forgetPreviousUsersLocalData';
import { supabase } from './supabaseClient';

/**
 * Same tag (`status`) and in-flight value (`loading`) as `SessionState` in
 * `./useSession`, so both hooks in this folder read the same way. The error arm
 * carries its cause, classified into the `@shared/errors` error vocabulary,
 * so a caller can tell an unreachable auth server from a refused session.
 */
export type SignOutResult =
  | { status: 'idle' }
  | { status: 'loading' }
  | { status: 'ok' }
  | { status: 'error'; error: unknown };

/**
 * Supabase's auth errors are outside `apiFetch`'s vocabulary, so sign-out
 * translates them here the way `authorization()` does one layer down: every
 * reader downstream branches on `NetworkError`/`ApiError` alone.
 */
function classifySignOutFailure(error: unknown): unknown {
  if (isSessionFetchFailure(error)) {
    return new NetworkError('transport', 'sign-out could not reach the auth server');
  }
  const status = (error as { status?: unknown } | null | undefined)?.status;
  if (typeof status !== 'number' || status === 0) return error;
  return new ApiError(status, `sign-out was refused with ${status}`);
}

/**
 * The most a failed sign-out may carry into a log, redacted like `apiFetch`'s
 * own `logFailure`. Never the caught error itself: an auth error's message is a
 * server string and its stack holds local paths, and this is the module that
 * handles the session token.
 */
function signOutFailureFields(cause: unknown): { status: number } | { failure: string } {
  if (cause instanceof ApiError) return { status: cause.status };
  if (cause instanceof NetworkError) return { failure: cause.failure };
  return { failure: 'unknown' };
}

/** The error state, and the one diagnostic line the failure leaves behind. */
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
      forgetPreviousUsersLocalData(queryClient);
      setState(error ? signOutFailed(error) : { status: 'ok' });
    } catch (error) {
      forgetPreviousUsersLocalData(queryClient);
      setState(signOutFailed(error));
    }
  }

  return { state, signOut };
}
