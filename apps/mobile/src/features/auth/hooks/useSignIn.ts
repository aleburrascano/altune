import { supabase } from '@shared/auth/supabaseClient';

import { lockoutOnRepeatedFailure } from '../attemptLockout';
import type { AuthErrorReason } from '../errorReason';
import {
  isInvalidCredentialsError,
  isRateLimitedAuthError,
  isTransportAuthError,
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

// Only a rejection GoTrue named as such accuses the password; anything else it
// refuses the request for — an unconfirmed address, a rate limit below 429, a
// code shipped after this was written — is `unknown` (#1646).
function signInErrorReason(error: SupabaseAuthErrorLike): SignInErrorReason {
  if (isRateLimitedAuthError(error)) return 'too_many_attempts';
  if (isTransportAuthError(error)) return 'network';
  if (isUnconfirmedEmailError(error)) return 'email_not_confirmed';
  if (isInvalidCredentialsError(error)) return 'invalid_credentials';
  return 'unknown';
}

export function useSignIn() {
  const { state, run } = useAsyncAuthAction<SignInResult, [string, string]>(
    lockoutOnRepeatedFailure('sign-in', async (email: string, password: string) => {
      const { error } = await supabase.auth.signInWithPassword({ email, password });
      if (!error) return { kind: 'ok' } as const;
      return { kind: 'error', reason: signInErrorReason(error) } as const;
    }),
  );

  return { state, signIn: run };
}
