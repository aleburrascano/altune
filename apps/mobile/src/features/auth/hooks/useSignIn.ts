import { supabase } from '@shared/auth/supabaseClient';

import { lockoutOnRepeatedFailure } from '../attemptLockout';
import type { SignInFailureReason } from '../errorReason';
import { reportSignInFailure } from '../reportSignInFailure';
import {
  isInvalidCredentialsError,
  classifyAuthError,
  isUnconfirmedEmailError,
  type SupabaseAuthErrorLike,
} from '../supabaseAuthError';

import { useAsyncAuthAction } from './useAsyncAuthAction';

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
  const { state, run } = useAsyncAuthAction<SignInResult, [string, string]>(
    lockoutOnRepeatedFailure(
      'sign-in',
      async (email: string, password: string) => {
        const { error } = await supabase.auth.signInWithPassword({ email, password });
        if (!error) return { kind: 'ok' } as const;
        const reason = signInErrorReason(error);
        reportSignInFailure(reason);
        return { kind: 'error', reason } as const;
      },
      (outcome) => outcome.kind === 'error' && outcome.reason === 'invalid_credentials',
    ),
  );

  return { state, signIn: run };
}
