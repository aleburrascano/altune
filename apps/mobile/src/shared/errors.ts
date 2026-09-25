export class ApiError extends Error {
  constructor(
    public readonly status: number,
    message: string,
    public readonly code?: string,
    public readonly correlationId?: string,
  ) {
    super(message);
    this.name = 'ApiError';
  }
}

export class ContractError extends Error {
  constructor(
    public readonly at: string,
    detail: string,
  ) {
    super(`API contract violation at ${at}: ${detail}`);
    this.name = 'ContractError';
  }
}

export type NetworkFailure = 'timeout' | 'transport';

export class NetworkError extends Error {
  constructor(
    public readonly failure: NetworkFailure,
    message: string,
    public readonly correlationId?: string,
  ) {
    super(message);
    this.name = 'NetworkError';
  }
}

/**
 * The id `apiFetch` sent in `X-Correlation-ID`, so a report of this failure can
 * be matched to the server's log lines. Absent where the request never carried
 * one (web) or the throw came from outside this client.
 */
export function correlationIdOf(error: unknown): string | undefined {
  if (error instanceof ApiError || error instanceof NetworkError) return error.correlationId;
  return undefined;
}

export function isSessionFetchFailure(error: unknown): boolean {
  if (typeof error !== 'object' || error === null) return false;
  return (error as { name?: string }).name === 'AuthRetryableFetchError';
}

export function isAbort(error: unknown): boolean {
  if (typeof error !== 'object' || error === null) return false;
  return (error as { name?: string }).name === 'AbortError';
}

export function isTelemetryGated(error: unknown): boolean {
  if (typeof error !== 'object' || error === null) return false;
  return (error as { name?: string }).name === 'TelemetryGatedError';
}

export function isRetryable(error: unknown): boolean {
  if (error instanceof NetworkError) return true;
  if (error instanceof ApiError) return error.status === 429 || error.status >= 500;
  return false;
}
