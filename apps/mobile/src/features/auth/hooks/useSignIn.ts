import { supabase } from '@shared/auth/supabaseClient';

import type { AuthErrorReason } from '../errorCopy';
import { isTransportAuthError } from '../supabaseAuthError';

import { useAsyncAuthAction } from './useAsyncAuthAction';

export type SignInResult =
  | { kind: 'idle' }
  | { kind: 'pending' }
  | { kind: 'ok' }
  | { kind: 'error'; reason: Extract<AuthErrorReason, 'invalid_credentials' | 'network' | 'unknown'> };

export function useSignIn() {
  const { state, run } = useAsyncAuthAction<SignInResult, [string, string]>(
    async (email, password) => {
      const { error } = await supabase.auth.signInWithPassword({ email, password });
      if (!error) return { kind: 'ok' };
      return {
        kind: 'error',
        reason: isTransportAuthError(error) ? 'network' : 'invalid_credentials',
      };
    },
  );

  return { state, signIn: run };
}
