import { startDeadline } from '@shared/deadline/deadline';
import type { Deadline } from '@shared/deadline/deadline';
import { NetworkError } from '@shared/errors';

export const AUTH_ACTION_TIMEOUT_MS = 20_000;

function deadlineExpiry(deadline: Deadline, ms: number): Promise<never> {
  return new Promise<never>((_, reject) => {
    deadline.signal.addEventListener('abort', () => {
      reject(new NetworkError('timeout', `auth call exceeded the ${ms}ms timeout`));
    });
  });
}

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
