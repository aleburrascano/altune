import { supabase } from '@shared/auth/supabaseClient';

import { useAsyncAuthAction } from './useAsyncAuthAction';

export type UpdatePasswordResult =
  | { kind: 'idle' }
  | { kind: 'pending' }
  | { kind: 'ok' }
  | { kind: 'error'; reason: 'network' | 'unknown' };

export function useUpdatePassword() {
  const { state, run } = useAsyncAuthAction<UpdatePasswordResult, [string]>(async (password) => {
    const { error } = await supabase.auth.updateUser({ password });
    return error ? { kind: 'error', reason: 'unknown' } : { kind: 'ok' };
  });

  return { state, updatePassword: run };
}
