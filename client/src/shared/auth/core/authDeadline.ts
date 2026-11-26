import { startDeadline } from '@shared/timing';
import type { Deadline } from '@shared/timing';
import { NetworkError } from '@shared/failure';

const AUTH_CALL_TIMEOUT_MS = 15_000;

const AUTH_ACTION_TIMEOUT_MS = 20_000;

function timeoutError(what: string, correlationId?: string): NetworkError {
  const message = `${what} timed out after ${AUTH_CALL_TIMEOUT_MS}ms`;
  return new NetworkError('timeout', message, correlationId);
}

export function withinAuthDeadline<T>(work: Promise<T>, what: string, cid?: string): Promise<T> {
  return new Promise<T>((resolve, reject) => {
    const expire = (): void => reject(timeoutError(what, cid));
    const timer = setTimeout(expire, AUTH_CALL_TIMEOUT_MS);
    work.finally(() => clearTimeout(timer)).then(resolve, reject);
  });
}

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
