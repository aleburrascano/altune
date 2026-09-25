import { startDeadline } from '@shared/deadline/deadline';
import { NetworkError } from '@shared/errors';

export const AUTH_CALL_TIMEOUT_MS = 15_000;

export const AUTH_FETCH_TIMEOUT_MS = 10_000;

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

type FetchInput = Parameters<typeof fetch>[0];

export function fetchWithinAuthDeadline(input: FetchInput, init?: RequestInit): Promise<Response> {
  const deadline = startDeadline(init?.signal ?? undefined, AUTH_FETCH_TIMEOUT_MS);
  return fetch(input, { ...init, signal: deadline.signal }).finally(deadline.release);
}
