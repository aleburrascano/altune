import { useRef, useState } from 'react';

import { isNetworkError } from '@shared/lib/isNetworkError';

import { withAuthDeadline } from '../authDeadline';
import { thrownErrorDetail } from '../errorDetail';

function reportUnrecognizedFailure(err: unknown): void {
  console.warn('[auth] an auth action failed for an unrecognized reason', thrownErrorDetail(err));
}

export function useAsyncAuthAction<
  S extends { kind: 'idle' } | { kind: 'pending' } | { kind: string },
  A extends unknown[],
>(
  attempt: (...args: A) => Promise<Exclude<S, { kind: 'idle' } | { kind: 'pending' }>>,
): { state: S; run: (...args: A) => Promise<void> } {
  const [state, setState] = useState<S>({ kind: 'idle' } as S);
  const inFlight = useRef(false);

  async function run(...args: A): Promise<void> {
    if (inFlight.current) return;
    inFlight.current = true;
    setState({ kind: 'pending' } as S);
    try {
      setState(await withAuthDeadline(attempt(...args)));
    } catch (err) {
      const reason = isNetworkError(err) ? 'network' : 'unknown';
      if (reason === 'unknown') {
        reportUnrecognizedFailure(err);
      }
      setState({ kind: 'error', reason } as unknown as S);
    } finally {
      inFlight.current = false;
    }
  }

  return { state, run };
}
