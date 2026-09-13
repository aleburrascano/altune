import { useState } from 'react';

import { startDeadline } from '@shared/api-client/deadline';
import type { Deadline } from '@shared/api-client/deadline';
import { NetworkError } from '@shared/api-client/errors';
import { isNetworkError } from '@shared/lib/isNetworkError';

/**
 * UX budget for a single auth SDK call. A stalled network must never leave the
 * submit button pinned at `pending` forever — past this the call is abandoned
 * and mapped to a `network` error, which every auth hook already renders.
 */
export const AUTH_ACTION_TIMEOUT_MS = 20_000;

/** Rejects with a timeout `NetworkError` the moment the deadline aborts. */
function deadlineExpiry(deadline: Deadline): Promise<never> {
  return new Promise<never>((_, reject) => {
    deadline.signal.addEventListener('abort', () => {
      reject(new NetworkError('timeout', `auth call exceeded the ${AUTH_ACTION_TIMEOUT_MS}ms timeout`));
    });
  });
}

/**
 * The idle/pending/catch envelope every auth hook shares: start `idle`, flip to
 * `pending` on invoke, run the SDK call, and map a thrown transport failure to a
 * `network` / `unknown` error state. `attempt` owns only the SDK-specific middle
 * — it returns the resulting terminal state (`ok`, `sent`, an SDK error, …).
 *
 * The `attempt` call is raced against a deadline so a Supabase call that never
 * resolves resolves to a terminal `network` error instead of hanging forever.
 */
export function useAsyncAuthAction<
  S extends { kind: 'idle' } | { kind: 'pending' } | { kind: string },
  A extends unknown[],
>(
  attempt: (...args: A) => Promise<Exclude<S, { kind: 'idle' } | { kind: 'pending' }>>,
): { state: S; run: (...args: A) => Promise<void> } {
  const [state, setState] = useState<S>({ kind: 'idle' } as S);

  async function run(...args: A): Promise<void> {
    setState({ kind: 'pending' } as S);
    const deadline = startDeadline(undefined, AUTH_ACTION_TIMEOUT_MS);
    try {
      setState(await Promise.race([attempt(...args), deadlineExpiry(deadline)]));
    } catch (err) {
      const reason = isNetworkError(err) ? 'network' : 'unknown';
      setState({ kind: 'error', reason } as unknown as S);
    } finally {
      deadline.release();
    }
  }

  return { state, run };
}
