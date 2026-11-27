import type { ApiErrorCode } from '@shared/failure/core/apiErrorCodes';
import {
  HTTP_SERVER_ERROR_STATUS,
  HTTP_TOO_MANY_REQUESTS,
  HTTP_UNAUTHORIZED,
} from '@shared/failure/core/httpStatus';

export function isServerFaultStatus(status: number): boolean {
  return status >= HTTP_SERVER_ERROR_STATUS;
}

export interface ApiErrorExtras {
  readonly rawCode?: string;
  readonly detail?: string;
  readonly retryAfterMs?: number;
}

export class ApiError extends Error {
  readonly rawCode: string | undefined;
  readonly detail: string | undefined;
  readonly retryAfterMs: number | undefined;

  constructor(
    public readonly status: number,
    message: string,
    public readonly code?: ApiErrorCode,
    public readonly correlationId?: string,
    extras: ApiErrorExtras = {},
  ) {
    super(message);
    this.name = 'ApiError';
    this.rawCode = extras.rawCode;
    this.detail = extras.detail;
    this.retryAfterMs = extras.retryAfterMs;
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

const CONTRACT_ERROR_NAME: ContractError['name'] = 'ContractError';

export function isContractError(err: unknown): boolean {
  return err instanceof Error && err.name === CONTRACT_ERROR_NAME;
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

export function correlationIdOf(error: unknown): string | undefined {
  if (error instanceof ApiError || error instanceof NetworkError) return error.correlationId;
  return undefined;
}

export function isSessionRefusal(error: unknown): boolean {
  return error instanceof ApiError && error.status === HTTP_UNAUTHORIZED;
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
  if (error instanceof ApiError)
    return error.status === HTTP_TOO_MANY_REQUESTS || isServerFaultStatus(error.status);
  return false;
}

const NATIVE_FETCH_FAILURE_MESSAGES: ReadonlySet<string> = new Set([
  'Network request failed',
  'Failed to fetch',
  'Load failed',
]);

function isNativeFetchFailure(err: unknown): boolean {
  return err instanceof TypeError && NATIVE_FETCH_FAILURE_MESSAGES.has(err.message);
}

export function isNetworkError(err: unknown): boolean {
  return err instanceof NetworkError || isSessionFetchFailure(err) || isNativeFetchFailure(err);
}
