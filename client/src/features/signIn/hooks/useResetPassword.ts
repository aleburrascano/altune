import { requestPasswordReset } from '@shared/auth';

import { lockoutOnRepeatedFailure } from '@features/signIn/shell/attemptLockout';
import {
  classifyAuthError,
  type SupabaseAuthErrorLike,
  supabaseErrorDetail,
  type TransientAuthReason,
} from '@features/signIn/core/authError';
import { authRedirectUrl } from '@features/signIn/core/authRedirect';

import { useAsyncAuthAction } from '@features/signIn/shell/useAsyncAuthAction';

type ResetRequestResult =
  | { kind: 'idle' }
  | { kind: 'pending' }
  | { kind: 'sent' }
  | {
      kind: 'error';
      reason: TransientAuthReason;
    };

async function requestReset(email: string) {
  const { error } = await requestPasswordReset(email.trim(), {
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
