import { supabase } from '@shared/auth/supabaseClient';

import type { AuthErrorReason } from '../errorReason';
import { authRedirectUrl } from '../parseAuthLink';
import {
  isAlreadyRegisteredError,
  isRateLimitedAuthError,
  isTransportAuthError,
  isWeakPasswordError,
} from '../supabaseAuthError';

import { useAsyncAuthAction } from './useAsyncAuthAction';

export type SignUpResult =
  | { kind: 'idle' }
  | { kind: 'pending' }
  | { kind: 'ok' }
  | { kind: 'awaiting-confirmation' }
  | {
      kind: 'error';
      reason: Extract<
        AuthErrorReason,
        'already_registered' | 'weak_password' | 'network' | 'unknown' | 'too_many_attempts'
      >;
    };

type SettledSignUp = Exclude<SignUpResult, { kind: 'idle' | 'pending' }>;

type SignUpResponseData = { user?: { identities?: unknown } | null; session?: unknown } | null;

function signUpOutcome(data: SignUpResponseData): SettledSignUp {
  if (data?.session) return { kind: 'ok' };
  const identities = data?.user?.identities;
  if (!Array.isArray(identities)) return { kind: 'error', reason: 'unknown' };
  return identities.length === 0
    ? { kind: 'error', reason: 'already_registered' }
    : { kind: 'awaiting-confirmation' };
}

export function useSignUp() {
  const { state, run } = useAsyncAuthAction<SignUpResult, [string, string]>(
    async (email, password) => {
      const { data, error } = await supabase.auth.signUp({
        email,
        password,
        options: { emailRedirectTo: authRedirectUrl('confirm') },
      });
      if (error) {
        if (isRateLimitedAuthError(error)) return { kind: 'error', reason: 'too_many_attempts' };
        if (isTransportAuthError(error)) return { kind: 'error', reason: 'network' };
        if (isWeakPasswordError(error)) return { kind: 'error', reason: 'weak_password' };
        if (isAlreadyRegisteredError(error)) return { kind: 'error', reason: 'already_registered' };
        return { kind: 'error', reason: 'unknown' };
      }
      return signUpOutcome(data);
    },
  );

  return { state, signUp: run };
}
