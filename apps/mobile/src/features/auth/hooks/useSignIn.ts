import { supabase } from '@shared/auth/supabaseClient';

import { useAsyncAuthAction } from './useAsyncAuthAction';

export type SignInResult =
  | { kind: 'idle' }
  | { kind: 'pending' }
  | { kind: 'ok' }
  | { kind: 'error'; reason: 'invalid_credentials' | 'network' | 'unknown' };

export function useSignIn() {
  const { state, run } = useAsyncAuthAction<SignInResult, [string, string]>(
    async (email, password) => {
      const { error } = await supabase.auth.signInWithPassword({ email, password });
      return error ? { kind: 'error', reason: 'invalid_credentials' } : { kind: 'ok' };
    },
  );

  return { state, signIn: run };
}
