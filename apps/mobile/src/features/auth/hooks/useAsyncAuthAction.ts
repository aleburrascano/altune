import { useRef, useState } from 'react';

import { isNetworkError } from '@shared/lib/isNetworkError';

import { withAuthDeadline } from '../authDeadline';
import { thrownErrorDetail } from '../errorDetail';

// `unknown` is the one error state with nothing behind it: the reason names no
// cause, and the state's shape belongs to the calling hook, so there is nowhere
// on it to put one. The error's own name and message go to the log instead —
// without them a production auth failure leaves no trace at all (#1647). Never
// the thrown value itself: an SDK error can hold the request that carried the
// password.
function reportUnrecognizedFailure(err: unknown): void {
  console.warn('[auth] an auth action failed for an unrecognized reason', thrownErrorDetail(err));
}

/**
 * The idle/pending/catch envelope every auth hook shares: start `idle`, flip to
 * `pending` on invoke, run the SDK call, and map a thrown transport failure to a
 * `network` / `unknown` error state. `attempt` owns only the SDK-specific middle
 * — it returns the resulting terminal state (`ok`, `sent`, an SDK error, …).
 *
 * The `attempt` call is raced against a deadline so a Supabase call that never
 * resolves resolves to a terminal `network` error instead of hanging forever.
 *
 * At most one attempt is in flight per hook: a `run` arriving while one is
 * pending is a no-op, so a double submit cannot reach Supabase twice. The guard
 * is a ref rather than `state`, because two presses in the same tick both read
 * the `state` React has not re-rendered yet — the very race the caller's
 * `disabled={pending}` loses.
 */
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
