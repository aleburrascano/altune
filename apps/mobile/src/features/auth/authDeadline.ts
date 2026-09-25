import { startDeadline } from '@shared/deadline/deadline';
import type { Deadline } from '@shared/deadline/deadline';
import { NetworkError } from '@shared/errors';

/**
 * UX budget for a single auth SDK call. A stalled network must never leave the
 * submit button pinned at `pending` forever — past this the call is abandoned
 * and mapped to a `network` error, which every auth hook already renders.
 */
export const AUTH_ACTION_TIMEOUT_MS = 20_000;

/** Rejects with a timeout `NetworkError` the moment the deadline aborts. */
function deadlineExpiry(deadline: Deadline, ms: number): Promise<never> {
  return new Promise<never>((_, reject) => {
    deadline.signal.addEventListener('abort', () => {
      reject(new NetworkError('timeout', `auth call exceeded the ${ms}ms timeout`));
    });
  });
}

/**
 * Bounds one leg of an auth flow, so every caller abandons a stalled SDK call
 * on the same terms. The timer is released once the race settles, so a call
 * that came back in time can never be overwritten by its own deadline; the
 * abandoned call stays subscribed by the race, so a rejection it produces later
 * is never an unhandled one.
 */
export async function withAuthDeadline<T>(
  work: Promise<T>,
  ms: number = AUTH_ACTION_TIMEOUT_MS,
): Promise<T> {
  const deadline = startDeadline(undefined, ms);
  try {
    return await Promise.race([work, deadlineExpiry(deadline, ms)]);
  } finally {
    deadline.release();
  }
}
