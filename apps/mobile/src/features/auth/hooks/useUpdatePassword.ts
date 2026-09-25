import { supabase } from '@shared/auth/supabaseClient';

import { withAuthDeadline } from '../authDeadline';
import {
  type SupabaseErrorDetail,
  type ThrownErrorDetail,
  supabaseErrorDetail,
  thrownErrorDetail,
} from '../errorDetail';
import type { AuthErrorReason } from '../errorReason';
import {
  isRateLimitedAuthError,
  isTransportAuthError,
  isWeakPasswordError,
} from '../supabaseAuthError';

import { useAsyncAuthAction } from './useAsyncAuthAction';

export type UpdatePasswordResult =
  | { kind: 'idle' }
  | { kind: 'pending' }
  | { kind: 'ok' }
  | {
      kind: 'error';
      reason: Extract<AuthErrorReason, 'weak_password' | 'network' | 'unknown' | 'too_many_attempts'>;
    };

const REVOKE_OTHERS_TIMEOUT_MS = 5_000;

function reportRevokeFailure(detail: ThrownErrorDetail | SupabaseErrorDetail): void {
  console.warn('[auth] revoking the other sessions after a password change failed', detail);
}

async function revokeOtherSessions(): Promise<void> {
  try {
    const { error } = await withAuthDeadline(
      supabase.auth.signOut({ scope: 'others' }),
      REVOKE_OTHERS_TIMEOUT_MS,
    );
    if (error) reportRevokeFailure(supabaseErrorDetail(error));
  } catch (err) {
    reportRevokeFailure(thrownErrorDetail(err));
  }
}

export function useUpdatePassword() {
  const { state, run } = useAsyncAuthAction<UpdatePasswordResult, [string]>(async (password) => {
    const { error } = await supabase.auth.updateUser({ password });
    if (!error) {
      void revokeOtherSessions();
      return { kind: 'ok' };
    }
    if (isRateLimitedAuthError(error)) return { kind: 'error', reason: 'too_many_attempts' };
    if (isTransportAuthError(error)) return { kind: 'error', reason: 'network' };
    if (isWeakPasswordError(error)) return { kind: 'error', reason: 'weak_password' };
    return { kind: 'error', reason: 'unknown' };
  });

  return { state, updatePassword: run };
}
