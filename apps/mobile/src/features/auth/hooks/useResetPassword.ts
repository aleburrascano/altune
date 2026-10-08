import { supabase } from '@shared/auth/supabaseClient';

import { lockoutOnRepeatedFailure } from '@features/auth/domain/attemptLockout';
import { supabaseErrorDetail } from '@features/auth/domain/errorDetail';
import type { TransientAuthReason } from '@features/auth/domain/errorReason';
import { authRedirectUrl } from '@features/auth/domain/authRedirect';
import { classifyAuthError, type SupabaseAuthErrorLike } from '@features/auth/domain/supabaseAuthError';

import { useAsyncAuthAction } from '@features/auth/hooks/useAsyncAuthAction';

type ResetRequestResult =
  | { kind: 'idle' }
  | { kind: 'pending' }
  | { kind: 'sent' }
  | {
      kind: 'error';
      reason: TransientAuthReason;
    };

async function requestReset(email: string) {
  const { error } = await supabase.auth.resetPasswordForEmail(email.trim(), {
    redirectTo: authRedirectUrl('recovery'),
  });
  if (error) return failure(error);
  return { kind: 'sent' } as const;
}

function failure(error: SupabaseAuthErrorLike) {
  console.warn('[auth] password reset request failed', supabaseErrorDetail(error));
  return { kind: 'error', reason: classifyAuthError(error) } as const;
}

export function useResetPassword() {
  const { state, run } = useAsyncAuthAction<ResetRequestResult, [string]>(
    lockoutOnRepeatedFailure(
      'reset-request',
      requestReset,
      (outcome) => outcome.kind === 'error' && outcome.reason === 'unknown',
    ),
  );

  return { state, requestReset: run };
}
