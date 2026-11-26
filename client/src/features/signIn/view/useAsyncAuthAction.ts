import { useRef, useState } from 'react';

import { warn } from '@shared/log';
import { isNetworkError } from '@shared/failure';

import { withAuthDeadline } from '@shared/auth';
import { thrownErrorDetail } from '@features/signIn/core/authError';

function reportUnrecognizedFailure(err: unknown): void {
  warn('[auth] an auth action failed for an unrecognized reason', thrownErrorDetail(err));
}

type ThrownAuthFailure = { kind: 'error'; reason: 'network' | 'unknown' };

export function useAsyncAuthAction<
  S extends { kind: 'idle' } | { kind: 'pending' } | ThrownAuthFailure | { kind: string },
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
    let started: Promise<unknown> = Promise.resolve();
    try {
      const work = attempt(...args);
      started = work;
      setState(await withAuthDeadline(work));
    } catch (err) {
      const reason = isNetworkError(err) ? 'network' : 'unknown';
      if (reason === 'unknown') {
        reportUnrecognizedFailure(err);
      }
      const failure: ThrownAuthFailure = { kind: 'error', reason };
      setState(failure as S);
    } finally {
      const release = () => {
        inFlight.current = false;
      };
      started.then(release, release);
    }
  }

  return { state, run };
}
