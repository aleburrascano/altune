import { supabase } from '@shared/auth/supabaseClient';

import { lockoutOnRepeatedFailure } from '../attemptLockout';
import type { AuthErrorReason } from '../errorReason';
import { reportSignInFailure } from '../reportSignInFailure';
import {
  isInvalidCredentialsError,
  classifyAuthError,
  isUnconfirmedEmailError,
  type SupabaseAuthErrorLike,
} from '../supabaseAuthError';

import { useAsyncAuthAction } from './useAsyncAuthAction';

type SignInErrorReason = Extract<
  AuthErrorReason,
  'invalid_credentials' | 'email_not_confirmed' | 'network' | 'unknown' | 'too_many_attempts'
>;

export type SignInResult =
  | { kind: 'idle' }
  | { kind: 'pending' }
  | { kind: 'ok' }
  | { kind: 'error'; reason: SignInErrorReason };

function signInErrorReason(error: SupabaseAuthErrorLike): SignInErrorReason {
  return classifyAuthError(error, [
    [isUnconfirmedEmailError, 'email_not_confirmed'],
    [isInvalidCredentialsError, 'invalid_credentials'],
  ]);
}

export function useSignIn() {
  const { state, run } = useAsyncAuthAction<SignInResult, [string, string]>(
    lockoutOnRepeatedFailure('sign-in', async (email: string, password: string) => {
      const { error } = await supabase.auth.signInWithPassword({ email, password });
      if (!error) return { kind: 'ok' } as const;
      const reason = signInErrorReason(error);
      reportSignInFailure(reason);
      return { kind: 'error', reason } as const;
    }),
  );

  return { state, signIn: run };
}
