import { supabase } from '@shared/auth/supabaseClient';

import { lockoutOnRepeatedFailure } from '../attemptLockout';
import type { AuthErrorReason } from '../errorReason';
import { isTransportAuthError } from '../supabaseAuthError';

import { useAsyncAuthAction } from './useAsyncAuthAction';

export type SignInResult =
  | { kind: 'idle' }
  | { kind: 'pending' }
  | { kind: 'ok' }
  | {
      kind: 'error';
      reason: Extract<
        AuthErrorReason,
        'invalid_credentials' | 'network' | 'unknown' | 'too_many_attempts'
      >;
    };

export function useSignIn() {
  const { state, run } = useAsyncAuthAction<SignInResult, [string, string]>(
    lockoutOnRepeatedFailure(async (email: string, password: string) => {
      const { error } = await supabase.auth.signInWithPassword({ email, password });
      if (!error) return { kind: 'ok' } as const;
      return {
        kind: 'error',
        reason: isTransportAuthError(error) ? 'network' : 'invalid_credentials',
      } as const;
    }),
  );

  return { state, signIn: run };
}
