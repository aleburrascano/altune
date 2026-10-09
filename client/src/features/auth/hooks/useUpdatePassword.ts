import { supabase } from '@shared/auth/supabaseClient';

import { withAuthDeadline } from '@features/auth/domain/authDeadline';
import {
  classifyAuthError,
  isWeakPasswordError,
  type SupabaseErrorDetail,
  supabaseErrorDetail,
  type ThrownErrorDetail,
  thrownErrorDetail,
  type TransientAuthReason,
} from '@features/auth/domain/authError';

import { useAsyncAuthAction } from '@features/auth/hooks/useAsyncAuthAction';

type UpdatePasswordResult =
  | { kind: 'idle' }
  | { kind: 'pending' }
  | { kind: 'ok'; othersRevoked: boolean }
  | {
      kind: 'error';
      reason: TransientAuthReason | 'weak_password';
    };

const REVOKE_OTHERS_TIMEOUT_MS = 5_000;

function reportRevokeFailure(detail: ThrownErrorDetail | SupabaseErrorDetail): void {
  console.warn('[auth] revoking the other sessions after a password change failed', detail);
}

async function requestRevoke(): Promise<SupabaseErrorDetail | null> {
  const { error } = await withAuthDeadline(
    supabase.auth.signOut({ scope: 'others' }),
    REVOKE_OTHERS_TIMEOUT_MS,
  );
  return error ? supabaseErrorDetail(error) : null;
}

async function revokeOtherSessionsOnce(): Promise<boolean> {
  try {
    const failure = await requestRevoke();
    if (failure) reportRevokeFailure(failure);
    return failure === null;
  } catch (err) {
    reportRevokeFailure(thrownErrorDetail(err));
    return false;
  }
}

async function revokeOtherSessions(): Promise<boolean> {
  return (await revokeOtherSessionsOnce()) || (await revokeOtherSessionsOnce());
}

export function useUpdatePassword() {
  const { state, run } = useAsyncAuthAction<UpdatePasswordResult, [string]>(async (password) => {
    const { error } = await supabase.auth.updateUser({ password });
    if (!error) {
      return { kind: 'ok', othersRevoked: await revokeOtherSessions() };
    }
    console.warn('[auth] password update failed', supabaseErrorDetail(error));
    return {
      kind: 'error',
      reason: classifyAuthError(error, [[isWeakPasswordError, 'weak_password']]),
    };
  });

  return { state, updatePassword: run };
}
