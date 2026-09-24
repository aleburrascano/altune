import { NetworkError } from '@shared/errors';

export const AUTH_CALL_TIMEOUT_MS = 15_000;

export const AUTH_FETCH_TIMEOUT_MS = 10_000;

export function withinAuthDeadline<T>(
  work: Promise<T>,
  what: string,
  correlationId?: string,
): Promise<T> {
  return new Promise<T>((resolve, reject) => {
    const timer = setTimeout(() => {
      reject(
        new NetworkError(
          'timeout',
          `${what} timed out after ${AUTH_CALL_TIMEOUT_MS}ms`,
          correlationId,
        ),
      );
    }, AUTH_CALL_TIMEOUT_MS);
    work.then(
      (value) => {
        clearTimeout(timer);
        resolve(value);
      },
      (error: unknown) => {
        clearTimeout(timer);
        reject(error);
      },
    );
  });
}

export function fetchWithinAuthDeadline(
  input: Parameters<typeof fetch>[0],
  init?: RequestInit,
): Promise<Response> {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), AUTH_FETCH_TIMEOUT_MS);
  const external = init?.signal ?? undefined;
  const relay = (): void => controller.abort();
  if (external?.aborted) relay();
  else external?.addEventListener('abort', relay);
  return fetch(input, { ...init, signal: controller.signal }).finally(() => {
    clearTimeout(timer);
    external?.removeEventListener('abort', relay);
  });
}
