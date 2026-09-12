import { supabase } from '@shared/auth/supabaseClient';

import { useAsyncAuthAction } from './useAsyncAuthAction';

export const RECOVERY_REDIRECT_URL = 'altune://auth/recovery';

export type ResetRequestResult =
  | { kind: 'idle' }
  | { kind: 'pending' }
  | { kind: 'sent' }
  | { kind: 'error'; reason: 'network' | 'unknown' };

export function useResetPassword() {
  const { state, run } = useAsyncAuthAction<ResetRequestResult, [string]>(async (email) => {
    await supabase.auth.resetPasswordForEmail(email.trim(), {
      redirectTo: RECOVERY_REDIRECT_URL,
    });
    return { kind: 'sent' };
  });

  return { state, requestReset: run };
}
