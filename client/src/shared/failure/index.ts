export { RETRY_TAIL, describeError } from '@shared/failure/core/describeError';
export type { ErrorCopy } from '@shared/failure/core/describeError';
export { API_ERROR_CODES, isApiErrorCode } from '@shared/failure/core/apiErrorCodes';
export type { ApiErrorCode } from '@shared/failure/core/apiErrorCodes';
export {
  ApiError,
  ContractError,
  NetworkError,
  correlationIdOf,
  isAbort,
  isContractError,
  isNetworkError,
  isRetryable,
  isServerFaultStatus,
  isSessionFetchFailure,
  isSessionRefusal,
} from '@shared/failure/core/errors';
export { failureKindOf, kindOfCode, kindOfStatus } from '@shared/failure/core/failureKind';
export type { FailureKind } from '@shared/failure/core/failureKind';
export { fail, ok } from '@shared/failure/core/result';
export type { Result } from '@shared/failure/core/result';
export type { ApiErrorExtras } from '@shared/failure/core/errors';
export {
  HTTP_CLIENT_ERROR_STATUS,
  HTTP_REDIRECT_STATUS,
  HTTP_REQUEST_TIMEOUT,
  HTTP_SERVER_ERROR_STATUS,
  HTTP_TOO_MANY_REQUESTS,
  HTTP_UNAUTHORIZED,
} from '@shared/failure/core/httpStatus';
