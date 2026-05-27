/**
 * useSignIn — wraps supabase.auth.signInWithPassword and exposes a typed
 * result for the SignInScreen to render.
 *
 * Per AC#3, the failure surface MUST NOT distinguish "unknown email" from
 * "wrong password" in user-facing wording — that's a user-enumeration vector
 * Supabase's defaults leak. Tests assert only testID="auth-error" + non-empty
 * text presence; this hook collapses all failure modes into a generic
 * `error` kind so the screen can render whatever copy it wants.
 */
import { useState } from 'react';

import { supabase } from '../api/supabaseClient';

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
        // Supabase exposes the reason via error.message / code; we collapse
        // them to a single classification so the UI never leaks which axis
        // failed (per AC#3's user-enumeration risk).
        setState({ kind: 'error', reason: 'invalid_credentials' });
        return;
      }
      setState({ kind: 'ok' });
    } catch (err) {
      // Fetch / connectivity / SDK-internal failures.
      const isNetwork =
        err instanceof Error &&
        /network|fetch|timeout|connection/i.test(err.message);
      setState({ kind: 'error', reason: isNetwork ? 'network' : 'unknown' });
    }
  }

  return { state, signIn };
}
