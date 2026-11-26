export { RETRY_TAIL, describeError } from '@shared/failure/core/describeError';
export { API_ERROR_CODES, isApiErrorCode } from '@shared/failure/core/apiErrorCodes';
export type { ApiErrorCode } from '@shared/failure/core/apiErrorCodes';
export {
  ApiError,
  ContractError,
  HTTP_UNAUTHORIZED,
  NetworkError,
  correlationIdOf,
  isAbort,
  isNetworkError,
  isRetryable,
  isServerFaultStatus,
  isSessionFetchFailure,
} from '@shared/failure/core/errors';
