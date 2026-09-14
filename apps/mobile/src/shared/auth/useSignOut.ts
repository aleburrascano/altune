import { useQueryClient, type QueryClient } from '@tanstack/react-query';
import { useState } from 'react';

import { useTrackStatusStore } from '@shared/acquisition/trackStatusStore';
import { clearOutbox } from '@shared/telemetry/outbox';

import { runSignOutCleanups } from './signOutCleanup';
import { supabase } from './supabaseClient';

export type SignOutResult =
  | { kind: 'idle' }
  | { kind: 'pending' }
  | { kind: 'ok' }
  | { kind: 'error' };

function forgetPreviousUsersLocalData(queryClient: QueryClient): void {
  queryClient.clear();
  useTrackStatusStore.getState().reset();
  clearOutbox();
  runSignOutCleanups();
}

export function useSignOut() {
  const queryClient = useQueryClient();
  const [state, setState] = useState<SignOutResult>({ kind: 'idle' });

  async function signOut(): Promise<void> {
    setState({ kind: 'pending' });
    try {
      const { error } = await supabase.auth.signOut();
      forgetPreviousUsersLocalData(queryClient);
      setState(error ? { kind: 'error' } : { kind: 'ok' });
    } catch {
      forgetPreviousUsersLocalData(queryClient);
      setState({ kind: 'error' });
    }
  }

  return { state, signOut };
}
