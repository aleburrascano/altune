/**
 * useSignUp — wraps supabase.auth.signUp.
 *
 * Per spec Design considerations: email confirmation is DISABLED in the
 * altune Supabase project for v1, so signUp returns a session immediately
 * (the SDK persists it via the secure-store adapter; useSession picks it up
 * and the root layout routes to /library). If a future spec re-enables
 * email confirmation, signUp will return session=null and this hook will
 * need a "check your email" state — not in v1 scope.
 */
import { useState } from 'react';

import { supabase } from '../api/supabaseClient';

export type SignUpResult =
  | { kind: 'idle' }
  | { kind: 'pending' }
  | { kind: 'ok' }
  | { kind: 'error'; reason: 'already_registered' | 'weak_password' | 'network' | 'unknown' };

export function useSignUp() {
  const [state, setState] = useState<SignUpResult>({ kind: 'idle' });

  async function signUp(email: string, password: string): Promise<void> {
    setState({ kind: 'pending' });
    try {
      const { error } = await supabase.auth.signUp({ email, password });
      if (error) {
        setState({ kind: 'error', reason: 'unknown' });
        return;
      }
      setState({ kind: 'ok' });
    } catch (err) {
      const isNetwork =
        err instanceof Error && /network|fetch|timeout|connection/i.test(err.message);
      setState({ kind: 'error', reason: isNetwork ? 'network' : 'unknown' });
    }
  }

  return { state, signUp };
}
