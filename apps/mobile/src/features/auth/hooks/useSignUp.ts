/**
 * useSignUp — wraps supabase.auth.signUp.
 *
 * When Supabase email-confirmation is ENABLED, signUp returns session=null
 * and a confirmation email is sent — we surface `awaiting-confirmation` so the
 * screen shows a "check your email" state (AC#4). When it's DISABLED (the v1
 * default until the dashboard setting is flipped), signUp returns a session
 * immediately and we report `ok` so the existing route-into-app flow still
 * works — the contract is gated on the response, not hard-coded.
 *
 * Anti-enumeration (AC#5): a sign-up for an already-registered address also
 * resolves to `awaiting-confirmation` (Supabase returns the same null-session
 * shape + sends an "account exists" email), so the UI is identical to a fresh
 * sign-up and never reveals that the email exists.
 */
import { useState } from 'react';

import { supabase } from '@shared/auth/supabaseClient';

import { isNetworkError } from '../lib/isNetworkError';

/** Confirmation link target — whitelisted in parseAuthLink + Supabase redirects. */
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
        // Collapse to a generic failure — never leak which axis failed.
        setState({ kind: 'error', reason: 'unknown' });
        return;
      }
      // No session => email confirmation is required (or the address already
      // exists). Either way: tell the user to check their email.
      setState(data?.session ? { kind: 'ok' } : { kind: 'awaiting-confirmation' });
    } catch (err) {
      setState({ kind: 'error', reason: isNetworkError(err) ? 'network' : 'unknown' });
    }
  }

  return { state, signUp };
}
