import { signInWithPassword } from '@shared/auth';

import {
  classifyAuthError,
  isInvalidCredentialsError,
  isUnconfirmedEmailError,
  type SignInFailureReason,
  type SupabaseAuthErrorLike,
} from '@features/signIn/core/errors';
import {
  useAttemptLockout,
  useAuthPort,
  useSignInHealth,
} from '@features/signIn/view/SignInProvider';

import { useAsyncAuthAction } from '@features/signIn/view/useAsyncAuthAction';

type SignInResult =
  | { kind: 'idle' }
  | { kind: 'pending' }
  | { kind: 'ok' }
  | { kind: 'error'; reason: SignInFailureReason };

function signInErrorReason(error: SupabaseAuthErrorLike): SignInFailureReason {
  return classifyAuthError(error, [
    [isUnconfirmedEmailError, 'email_not_confirmed'],
    [isInvalidCredentialsError, 'invalid_credentials'],
  ]);
}

export function useSignIn() {
  const authPort = useAuthPort();
  const lockout = useAttemptLockout();
  const health = useSignInHealth();
  const { state, run } = useAsyncAuthAction<SignInResult, [string, string]>(
    lockout.guard(
      'sign-in',
      async (email: string, password: string) => {
        const { error } = await signInWithPassword({ email, password });
        if (!error) {
          health.recordSignInOk();
          return { kind: 'ok' } as const;
        }
        const reason = signInErrorReason(error);
        authPort.reportSignInFailure(reason);
        return { kind: 'error', reason } as const;
      },
      (outcome) => outcome.kind === 'error' && outcome.reason === 'invalid_credentials',
    ),
  );

  return { state, signIn: run };
}
