import { useState } from 'react';

import { supabase } from '@shared/auth/supabaseClient';

import { isNetworkError } from '@shared/lib/isNetworkError';

export const CONFIRM_REDIRECT_URL = 'altune://auth/confirm';

export type SignUpResult =
  | { kind: 'idle' }
  | { kind: 'pending' }
  | { kind: 'ok' }
  | { kind: 'awaiting-confirmation' }
  | { kind: 'error'; reason: 'already_registered' | 'weak_password' | 'network' | 'unknown' };

export function useSignUp() {
  const [state, setState] = useState<SignUpResult>({ kind: 'idle' });

  async function signUp(email: string, password: string): Promise<void> {
    setState({ kind: 'pending' });
    try {
      const { data, error } = await supabase.auth.signUp({
        email,
        password,
        options: { emailRedirectTo: CONFIRM_REDIRECT_URL },
      });
      if (error) {
        setState({ kind: 'error', reason: 'unknown' });
        return;
      }
      setState(data?.session ? { kind: 'ok' } : { kind: 'awaiting-confirmation' });
    } catch (err) {
      setState({ kind: 'error', reason: isNetworkError(err) ? 'network' : 'unknown' });
    }
  }

  return { state, signUp };
}
