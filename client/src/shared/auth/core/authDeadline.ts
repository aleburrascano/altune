import { withDeadline } from '@shared/timing';
import { NetworkError } from '@shared/failure';

const AUTH_CALL_TIMEOUT_MS = 15_000;

const AUTH_ACTION_TIMEOUT_MS = 20_000;

function timeoutError(what: string, correlationId?: string): NetworkError {
  const message = `${what} timed out after ${AUTH_CALL_TIMEOUT_MS}ms`;
  return new NetworkError('timeout', message, correlationId);
}

export function withinAuthDeadline<T>(work: Promise<T>, what: string, cid?: string): Promise<T> {
  return withDeadline(work, AUTH_CALL_TIMEOUT_MS, { onExpire: () => timeoutError(what, cid) });
}

export function withAuthDeadline<T>(
  work: Promise<T>,
  ms: number = AUTH_ACTION_TIMEOUT_MS,
): Promise<T> {
  return withDeadline(work, ms, {
    onExpire: () => new NetworkError('timeout', `auth call exceeded the ${ms}ms timeout`),
  });
}
