import { supabase } from '@shared/auth/supabaseClient';

import type { AuthErrorReason } from '../errorReason';
import { isTransportAuthError, isWeakPasswordError } from '../supabaseAuthError';

import { useAsyncAuthAction } from './useAsyncAuthAction';

export type UpdatePasswordResult =
  | { kind: 'idle' }
  | { kind: 'pending' }
  | { kind: 'ok' }
  | { kind: 'error'; reason: Extract<AuthErrorReason, 'weak_password' | 'network' | 'unknown'> };

export function useUpdatePassword() {
  const { state, run } = useAsyncAuthAction<UpdatePasswordResult, [string]>(async (password) => {
    const { error } = await supabase.auth.updateUser({ password });
    if (!error) return { kind: 'ok' };
    if (isTransportAuthError(error)) return { kind: 'error', reason: 'network' };
    if (isWeakPasswordError(error)) return { kind: 'error', reason: 'weak_password' };
    return { kind: 'error', reason: 'unknown' };
  });

  return { state, updatePassword: run };
}
