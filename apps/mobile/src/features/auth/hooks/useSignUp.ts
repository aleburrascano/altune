import { supabase } from '@shared/auth/supabaseClient';

import { useAsyncAuthAction } from './useAsyncAuthAction';

export const CONFIRM_REDIRECT_URL = 'altune://auth/confirm';

export type SignUpResult =
  | { kind: 'idle' }
  | { kind: 'pending' }
  | { kind: 'ok' }
  | { kind: 'awaiting-confirmation' }
  | { kind: 'error'; reason: 'already_registered' | 'weak_password' | 'network' | 'unknown' };

export function useSignUp() {
  const { state, run } = useAsyncAuthAction<SignUpResult, [string, string]>(
    async (email, password) => {
      const { data, error } = await supabase.auth.signUp({
        email,
        password,
        options: { emailRedirectTo: CONFIRM_REDIRECT_URL },
      });
      if (error) return { kind: 'error', reason: 'unknown' };
      return data?.session ? { kind: 'ok' } : { kind: 'awaiting-confirmation' };
    },
  );

  return { state, signUp: run };
}
