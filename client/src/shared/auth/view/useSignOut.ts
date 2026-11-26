import { useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';

import { ApiError, NetworkError, isSessionFetchFailure } from '@shared/failure';

import { withinAuthDeadline } from '@shared/auth/core/authDeadline';
import { authSource } from '@shared/auth/ports/authSource';
import { forgetPreviousUsersLocalData } from '@shared/auth/view/userLocalData';
import { warn } from '@shared/log';

export type SignOutResult =
  | { status: 'idle' }
  | { status: 'loading' }
  | { status: 'ok' }
  | { status: 'error'; error: unknown };

function asSignOutFailure(error: unknown): unknown {
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
  const cause = asSignOutFailure(error);
  warn('[auth] sign out failed', signOutFailureFields(cause));
  return { status: 'error', error: cause };
}

async function endLocalSession(): Promise<void> {
  await authSource()
    .clearPersistedSession()
    .catch((error: unknown) =>
      warn('[auth] clearing persisted session failed', signOutFailureFields(error)),
    );
  await withinAuthDeadline(authSource().signOut({ scope: 'local' }), 'sign-out-local').catch(
    (error: unknown) => warn('[auth] local sign out failed', signOutFailureFields(error)),
  );
}

export function useSignOut() {
  const queryClient = useQueryClient();
  const [state, setState] = useState<SignOutResult>({ status: 'idle' });

  async function failSignOut(error: unknown): Promise<void> {
    await endLocalSession();
    forgetPreviousUsersLocalData(queryClient);
    setState(signOutFailed(error));
  }

  async function signOut(): Promise<void> {
    setState({ status: 'loading' });
    try {
      const { error } = await withinAuthDeadline(authSource().signOut(), 'sign-out');
      if (error) {
        await failSignOut(error);
        return;
      }
      forgetPreviousUsersLocalData(queryClient);
      setState({ status: 'ok' });
    } catch (error) {
      await failSignOut(error);
    }
  }

  return { state, signOut };
}
