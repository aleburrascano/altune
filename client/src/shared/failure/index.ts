export { RETRY_TAIL, describeError } from '@shared/failure/describeError';
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
  isTelemetryGated,
} from '@shared/failure/errors';
