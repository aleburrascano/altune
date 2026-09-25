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

export function useResetPassword() {
  const { state, run } = useAsyncAuthAction<ResetRequestResult, [string]>(
    lockoutOnRepeatedFailure('reset-request', async (email: string) => {
      const { error } = await supabase.auth.resetPasswordForEmail(email.trim(), {
        redirectTo: RECOVERY_REDIRECT_URL,
      });
      // Any resolved `{ error }` means the request never turned into a delivered
      // email, so it must not report `sent`. Supabase does not error on an unknown
      // address — it succeeds regardless — so surfacing the error here (rate
      // limiting, malformed request, transport) leaks nothing about enumeration.
      if (error) {
        return {
          kind: 'error',
          reason: isTransportAuthError(error) ? 'network' : 'unknown',
        } as const;
      }
      return { kind: 'sent' } as const;
    }),
  );

  return { state, requestReset: run };
}
