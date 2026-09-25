import { supabase } from '@shared/auth/supabaseClient';

import { lockoutOnRepeatedFailure } from '../attemptLockout';
import type { AuthErrorReason } from '../errorReason';
import { RECOVERY_REDIRECT_URL } from '../parseAuthLink';
import { isTransportAuthError } from '../supabaseAuthError';

import { useAsyncAuthAction } from './useAsyncAuthAction';

export type ResetRequestResult =
  | { kind: 'idle' }
  | { kind: 'pending' }
  | { kind: 'sent' }
  | {
      kind: 'error';
      reason: Extract<AuthErrorReason, 'network' | 'unknown' | 'too_many_attempts'>;
    };

async function requestReset(email: string) {
  const { error } = await supabase.auth.resetPasswordForEmail(email.trim(), {
    redirectTo: RECOVERY_REDIRECT_URL,
  });
  if (error) return failure(error);
  return { kind: 'sent' } as const;
}

function failure(error: Parameters<typeof isTransportAuthError>[0]) {
  return { kind: 'error', reason: isTransportAuthError(error) ? 'network' : 'unknown' } as const;
}

export function useResetPassword() {
  const { state, run } = useAsyncAuthAction<ResetRequestResult, [string]>(
    lockoutOnRepeatedFailure('reset-request', requestReset),
  );

  return { state, requestReset: run };
}
