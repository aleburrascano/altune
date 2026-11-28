import { warn } from '@shared/log';
import { requestPasswordReset } from '@shared/auth';

import { useAttemptLockout } from '@features/signIn/view/SignInProvider';
import {
  classifyAuthError,
  type SupabaseAuthErrorLike,
  supabaseErrorDetail,
  type TransientAuthReason,
} from '@features/signIn/core/errors';
import { authRedirectUrl } from '@features/signIn/core/authRedirect';

import { useAsyncAuthAction } from '@features/signIn/view/useAsyncAuthAction';

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
  warn('[auth] password reset request failed', supabaseErrorDetail(error));
  return { kind: 'error', reason: classifyAuthError(error) } as const;
}

export function useResetPassword() {
  const lockout = useAttemptLockout();
  const { state, run } = useAsyncAuthAction<ResetRequestResult, [string]>(
    lockout.guard(
      'reset-request',
      requestReset,
      (outcome) => outcome.kind === 'error' && outcome.reason === 'unknown',
    ),
  );

  return { state, requestReset: run };
}
