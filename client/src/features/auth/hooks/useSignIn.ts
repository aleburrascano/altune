import { signInWithPassword } from '@shared/auth/authActions';

import { lockoutOnRepeatedFailure } from '@features/auth/domain/attemptLockout';
import {
  classifyAuthError,
  isInvalidCredentialsError,
  isUnconfirmedEmailError,
  type SignInFailureReason,
  type SupabaseAuthErrorLike,
} from '@features/auth/domain/authError';
import { useAuthPort } from '@features/auth/ports/AuthPortProvider';

import { useAsyncAuthAction } from '@features/auth/hooks/useAsyncAuthAction';

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
  const { state, run } = useAsyncAuthAction<SignInResult, [string, string]>(
    lockoutOnRepeatedFailure(
      'sign-in',
      async (email: string, password: string) => {
        const { error } = await signInWithPassword({ email, password });
        if (!error) return { kind: 'ok' } as const;
        const reason = signInErrorReason(error);
        authPort.reportSignInFailure(reason);
        return { kind: 'error', reason } as const;
      },
      (outcome) => outcome.kind === 'error' && outcome.reason === 'invalid_credentials',
    ),
  );

  return { state, signIn: run };
}
