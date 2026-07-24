import { useState } from 'react';

import { supabase } from '@shared/auth/supabaseClient';

import { isNetworkError } from '@shared/lib/isNetworkError';

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
      setState({ kind: 'sent' });
    } catch (err) {
      setState({ kind: 'error', reason: isNetworkError(err) ? 'network' : 'unknown' });
    }
  }

  return { state, requestReset };
}
