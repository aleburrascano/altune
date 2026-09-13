import { supabase } from '@shared/auth/supabaseClient';

import type { AuthErrorReason } from '../lib/errorCopy';
import { isTransportAuthError } from '../lib/supabaseAuthError';

import { useAsyncAuthAction } from './useAsyncAuthAction';

export const RECOVERY_REDIRECT_URL = 'altune://auth/recovery';

export type ResetRequestResult =
  | { kind: 'idle' }
  | { kind: 'pending' }
  | { kind: 'sent' }
  | { kind: 'error'; reason: Extract<AuthErrorReason, 'network' | 'unknown'> };

export function useResetPassword() {
  const { state, run } = useAsyncAuthAction<ResetRequestResult, [string]>(async (email) => {
    const { error } = await supabase.auth.resetPasswordForEmail(email.trim(), {
      redirectTo: RECOVERY_REDIRECT_URL,
    });
    // A transport failure the SDK swallowed must not masquerade as a delivered
    // email. Every other outcome stays `sent` to avoid account enumeration.
    if (error && isTransportAuthError(error)) return { kind: 'error', reason: 'network' };
    return { kind: 'sent' };
  });

  return { state, requestReset: run };
}
