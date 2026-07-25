export class ApiError extends Error {
  constructor(
    public readonly status: number,
    message: string,
  ) {
    super(message);
    this.name = 'ApiError';
  }
}

export type NetworkFailure = 'timeout' | 'transport';

export class NetworkError extends Error {
  constructor(
    public readonly failure: NetworkFailure,
    message: string,
  ) {
    super(message);
    this.name = 'NetworkError';
  }
}

export function isSessionFetchFailure(error: unknown): boolean {
  if (typeof error !== 'object' || error === null) return false;
  return (error as { name?: string }).name === 'AuthRetryableFetchError';
}

export function isAbort(error: unknown): boolean {
  if (typeof error !== 'object' || error === null) return false;
  return (error as { name?: string }).name === 'AbortError';
}

export function isRetryable(error: unknown): boolean {
  if (error instanceof NetworkError) return true;
  if (error instanceof ApiError) return error.status === 429 || error.status >= 500;
  return false;
}
