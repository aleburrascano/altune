/**
 * useUpdatePassword — sets a new password for the active (recovery) session
 * via supabase.auth.updateUser. Reached from the set-new-password screen
 * after the recovery deep link established a session.
 */
import { useState } from 'react';

import { supabase } from '../api/supabaseClient';

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
      const isNetwork =
        err instanceof Error && /network|fetch|timeout|connection/i.test(err.message);
      setState({ kind: 'error', reason: isNetwork ? 'network' : 'unknown' });
    }
  }

  return { state, updatePassword };
}
