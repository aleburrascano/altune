import {
  serverKindOf,
  type ApiErrorCode,
  type ServerErrorKind,
} from '@shared/failure/core/apiErrorCodes';
import {
  HTTP_CLIENT_ERROR_STATUS,
  HTTP_CONFLICT,
  HTTP_FORBIDDEN,
  HTTP_GONE,
  HTTP_NOT_FOUND,
  HTTP_REQUEST_TIMEOUT,
  HTTP_SERVER_ERROR_STATUS,
  HTTP_TOO_MANY_REQUESTS,
  HTTP_UNAUTHORIZED,
} from '@shared/failure/core/httpStatus';
import {
  ApiError,
  NetworkError,
  isContractError,
  isNetworkError,
} from '@shared/failure/core/errors';

export type FailureKind =
  | 'offline'
  | 'timeout'
  | 'unauthorized'
  | 'not-found'
  | 'rate-limited'
  | 'conflict'
  | 'limit-reached'
  | 'server'
  | 'rejected'
  | 'contract'
  | 'unknown';

export function kindOfStatus(status: number): FailureKind {
  if (status === HTTP_UNAUTHORIZED || status === HTTP_FORBIDDEN) return 'unauthorized';
  if (status === HTTP_NOT_FOUND || status === HTTP_GONE) return 'not-found';
  if (status === HTTP_REQUEST_TIMEOUT) return 'timeout';
  if (status === HTTP_TOO_MANY_REQUESTS) return 'rate-limited';
  if (status === HTTP_CONFLICT) return 'conflict';
  if (status >= HTTP_SERVER_ERROR_STATUS) return 'server';
  if (status >= HTTP_CLIENT_ERROR_STATUS) return 'rejected';
  return 'unknown';
}

function kindOfServerKind(kind: ServerErrorKind): FailureKind {
  switch (kind) {
    case 'unauthenticated':
    case 'forbidden':
      return 'unauthorized';
    case 'not_found':
      return 'not-found';
    case 'conflict':
      return 'conflict';
    case 'too_large':
      return 'limit-reached';
    case 'rate_limited':
      return 'rate-limited';
    case 'timeout':
      return 'timeout';
    case 'invalid':
      return 'rejected';
    case 'internal':
    case 'upstream_failed':
    case 'unavailable':
      return 'server';
    default:
      return assertNever(kind);
  }
}

export function kindOfCode(code: ApiErrorCode): FailureKind {
  return kindOfServerKind(serverKindOf(code));
}

function assertNever(kind: never): FailureKind {
  throw new Error(`unhandled server error kind ${String(kind)}`);
}

export function failureKindOf(error: unknown): FailureKind {
  if (isContractError(error)) return 'contract';
  if (error instanceof NetworkError && error.failure === 'timeout') return 'timeout';
  if (isNetworkError(error)) return 'offline';
  if (!(error instanceof ApiError)) return 'unknown';
  const fromCode = error.code === undefined ? 'unknown' : kindOfCode(error.code);
  return fromCode === 'unknown' ? kindOfStatus(error.status) : fromCode;
}
