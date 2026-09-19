import { useQueryClient, type QueryClient } from '@tanstack/react-query';
import { useState } from 'react';

import { useDownloadStore } from '@shared/acquisition/downloadStore';
import { useTrackStatusStore } from '@shared/acquisition/trackStatusStore';
import { runSignOutCleanups } from '@shared/session/signOutCleanup';
import { clearOutbox } from '@shared/telemetry/outbox';

import { supabase } from './supabaseClient';

/**
 * Same tag (`status`) and in-flight value (`loading`) as `SessionState` in
 * `./useSession`, so both hooks in this folder read the same way.
 */
export type SignOutResult =
  | { status: 'idle' }
  | { status: 'loading' }
  | { status: 'ok' }
  | { status: 'error' };

function forgetPreviousUsersLocalData(queryClient: QueryClient): void {
  queryClient.clear();
  useDownloadStore.getState().reset();
  useTrackStatusStore.getState().reset();
  clearOutbox();
  runSignOutCleanups();
}

export function useSignOut() {
  const queryClient = useQueryClient();
  const [state, setState] = useState<SignOutResult>({ status: 'idle' });

  async function signOut(): Promise<void> {
    setState({ status: 'loading' });
    try {
      const { error } = await supabase.auth.signOut();
      forgetPreviousUsersLocalData(queryClient);
      setState(error ? { status: 'error' } : { status: 'ok' });
    } catch {
      forgetPreviousUsersLocalData(queryClient);
      setState({ status: 'error' });
    }
  }

  return { state, signOut };
}
