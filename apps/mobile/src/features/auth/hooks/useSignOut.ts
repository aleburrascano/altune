/**
 * useSignOut — drops the Supabase session AND clears the React Query cache.
 *
 * The cache clear is the AC#5(b) half: a representative authenticated query
 * MUST refetch after sign-out + sign-in (a stale cached result from user A
 * leaking to user B would be a multi-tenancy bug at the cache layer). Once
 * supabase.auth.signOut resolves, useSession flips to signed-out and the
 * root AuthGate redirects to /sign-in.
 */
import { useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';

import { supabase } from '../api/supabaseClient';

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
      // Clear the cache regardless of SDK outcome — the user pressed sign-out
      // and we want their data off this device.
      queryClient.clear();
      setState(error ? { kind: 'error' } : { kind: 'ok' });
    } catch {
      queryClient.clear();
      setState({ kind: 'error' });
    }
  }

  return { state, signOut };
}
