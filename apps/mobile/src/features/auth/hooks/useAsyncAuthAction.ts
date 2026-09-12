import { useState } from 'react';

import { isNetworkError } from '@shared/lib/isNetworkError';

/**
 * The idle/pending/catch envelope every auth hook shares: start `idle`, flip to
 * `pending` on invoke, run the SDK call, and map a thrown transport failure to a
 * `network` / `unknown` error state. `attempt` owns only the SDK-specific middle
 * — it returns the resulting terminal state (`ok`, `sent`, an SDK error, …).
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
    try {
      setState(await attempt(...args));
    } catch (err) {
      const reason = isNetworkError(err) ? 'network' : 'unknown';
      setState({ kind: 'error', reason } as unknown as S);
    }
  }

  return { state, run };
}
