import { supabase } from '@shared/auth/supabaseClient';

import { lockoutOnRepeatedFailure } from '../attemptLockout';
import type { AuthErrorReason } from '../errorReason';
import { authRedirectUrl } from '../parseAuthLink';
import { isRateLimitedAuthError, isTransportAuthError } from '../supabaseAuthError';

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
    redirectTo: authRedirectUrl('recovery'),
  });
  if (error) return failure(error);
  return { kind: 'sent' } as const;
}

function failure(error: Parameters<typeof isTransportAuthError>[0]) {
  if (isRateLimitedAuthError(error)) return { kind: 'error', reason: 'too_many_attempts' } as const;
  return { kind: 'error', reason: isTransportAuthError(error) ? 'network' : 'unknown' } as const;
}

export function useResetPassword() {
  const { state, run } = useAsyncAuthAction<ResetRequestResult, [string]>(
    lockoutOnRepeatedFailure('reset-request', requestReset),
  );

  return { state, requestReset: run };
}
