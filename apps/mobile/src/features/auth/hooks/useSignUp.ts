import { supabase } from '@shared/auth/supabaseClient';

import type { AuthErrorReason } from '../errorCopy';
import { CONFIRM_REDIRECT_URL } from '../parseAuthLink';
import {
  isAlreadyRegisteredError,
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
      reason: Extract<AuthErrorReason, 'already_registered' | 'weak_password' | 'network' | 'unknown'>;
    };

export function useSignUp() {
  const { state, run } = useAsyncAuthAction<SignUpResult, [string, string]>(
    async (email, password) => {
      const { data, error } = await supabase.auth.signUp({
        email,
        password,
        options: { emailRedirectTo: CONFIRM_REDIRECT_URL },
      });
      if (error) {
        if (isTransportAuthError(error)) return { kind: 'error', reason: 'network' };
        if (isWeakPasswordError(error)) return { kind: 'error', reason: 'weak_password' };
        if (isAlreadyRegisteredError(error)) return { kind: 'error', reason: 'already_registered' };
        return { kind: 'error', reason: 'unknown' };
      }
      // With email confirmation on, Supabase hides account enumeration by
      // resolving without an error but returning a user whose `identities` is
      // empty when the email is already registered — the only signal we get.
      if (data?.user && data.user.identities?.length === 0) {
        return { kind: 'error', reason: 'already_registered' };
      }
      return data?.session ? { kind: 'ok' } : { kind: 'awaiting-confirmation' };
    },
  );

  return { state, signUp: run };
}
