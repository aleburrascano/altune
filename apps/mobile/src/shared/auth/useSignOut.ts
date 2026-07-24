import { useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';

import { supabase } from './supabaseClient';

export type SignOutResult =
  | { kind: 'idle' }
  | { kind: 'pending' }
  | { kind: 'ok' }
  | { kind: 'error' };

export function useSignOut() {
  const queryClient = useQueryClient();
  const [state, setState] = useState<SignOutResult>({ kind: 'idle' });

  async function signOut(): Promise<void> {
    setState({ kind: 'pending' });
    try {
      const { error } = await supabase.auth.signOut();
      queryClient.clear();
      setState(error ? { kind: 'error' } : { kind: 'ok' });
    } catch {
      queryClient.clear();
      setState({ kind: 'error' });
    }
  }

  return { state, signOut };
}
