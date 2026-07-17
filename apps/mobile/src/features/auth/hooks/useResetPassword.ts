/**
 * useResetPassword — requests a password-reset email via Supabase.
 *
 * Anti-enumeration (AC#5/AC#6): the result is `sent` for ANY resolved
 * response — whether or not the address has an account. Only a thrown
 * transport failure surfaces as `error`. The reset email's link points at
 * `altune://auth/recovery`, handled by the deep-link spine.
 */
import { useState } from 'react';

import { supabase } from '@shared/auth/supabaseClient';

import { isNetworkError } from '../lib/isNetworkError';

/** Must match a whitelisted path in parseAuthLink + a Supabase redirect URL. */
export const RECOVERY_REDIRECT_URL = 'altune://auth/recovery';

export type ResetRequestResult =
  | { kind: 'idle' }
  | { kind: 'pending' }
  | { kind: 'sent' }
  | { kind: 'error'; reason: 'network' | 'unknown' };

export function useResetPassword() {
  const [state, setState] = useState<ResetRequestResult>({ kind: 'idle' });

  async function requestReset(email: string): Promise<void> {
    setState({ kind: 'pending' });
    try {
      await supabase.auth.resetPasswordForEmail(email.trim(), {
        redirectTo: RECOVERY_REDIRECT_URL,
      });
      // Always 'sent' — never reveal whether the email exists.
      setState({ kind: 'sent' });
    } catch (err) {
      setState({ kind: 'error', reason: isNetworkError(err) ? 'network' : 'unknown' });
    }
  }

  return { state, requestReset };
}
