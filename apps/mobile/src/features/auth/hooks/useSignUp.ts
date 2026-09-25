import { supabase } from '@shared/auth/supabaseClient';

import type { AuthErrorReason } from '../errorReason';
import { CONFIRM_REDIRECT_URL } from '../parseAuthLink';
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

// `identities` is read as `unknown` rather than as the SDK's array, because what
// it holds at runtime is the very thing in question below.
type SignUpResponseData = { user?: { identities?: unknown } | null; session?: unknown } | null;

// The only "already registered" signal GoTrue gives while email confirmation is
// hiding account enumeration: it resolves without an error and returns a user
// whose `identities` is empty. No version of `AuthResponse` promises that, so it
// is recognised positively — a response that stops carrying it is `unknown`, not
// a coin flip between "already registered" and "check your inbox" (#1650).
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
        options: { emailRedirectTo: CONFIRM_REDIRECT_URL },
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
