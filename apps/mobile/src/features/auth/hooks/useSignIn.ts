import { useState } from 'react';

import { supabase } from '@shared/auth/supabaseClient';

import { isNetworkError } from '@shared/lib/isNetworkError';

export type SignInResult =
  | { kind: 'idle' }
  | { kind: 'pending' }
  | { kind: 'ok' }
  | { kind: 'error'; reason: 'invalid_credentials' | 'network' | 'unknown' };

export function useSignIn() {
  const [state, setState] = useState<SignInResult>({ kind: 'idle' });

  async function signIn(email: string, password: string): Promise<void> {
    setState({ kind: 'pending' });
    try {
      const { error } = await supabase.auth.signInWithPassword({ email, password });
      if (error) {
        setState({ kind: 'error', reason: 'invalid_credentials' });
        return;
      }
      setState({ kind: 'ok' });
    } catch (err) {
      setState({ kind: 'error', reason: isNetworkError(err) ? 'network' : 'unknown' });
    }
  }

  return { state, signIn };
}
