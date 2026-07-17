/**
 * useUpdatePassword — sets a new password for the active (recovery) session
 * via supabase.auth.updateUser. Reached from the set-new-password screen
 * after the recovery deep link established a session.
 */
import { useState } from 'react';

import { supabase } from '@shared/auth/supabaseClient';

import { isNetworkError } from '../lib/isNetworkError';

export type UpdatePasswordResult =
  | { kind: 'idle' }
  | { kind: 'pending' }
  | { kind: 'ok' }
  | { kind: 'error'; reason: 'network' | 'unknown' };

export function useUpdatePassword() {
  const [state, setState] = useState<UpdatePasswordResult>({ kind: 'idle' });

  async function updatePassword(password: string): Promise<void> {
    setState({ kind: 'pending' });
    try {
      const { error } = await supabase.auth.updateUser({ password });
      if (error) {
        setState({ kind: 'error', reason: 'unknown' });
        return;
      }
      setState({ kind: 'ok' });
    } catch (err) {
      setState({ kind: 'error', reason: isNetworkError(err) ? 'network' : 'unknown' });
    }
  }

  return { state, updatePassword };
}
