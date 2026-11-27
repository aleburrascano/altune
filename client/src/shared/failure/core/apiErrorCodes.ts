import {
  API_ERROR_CODES,
  API_ERROR_CODE_KINDS,
} from '@shared/failure/core/apiErrorCodes.generated';

export { API_ERROR_CODES };

export type ApiErrorCode = (typeof API_ERROR_CODES)[number];
export type ServerErrorKind = (typeof API_ERROR_CODE_KINDS)[ApiErrorCode];

export function serverKindOf(code: ApiErrorCode): ServerErrorKind {
  return API_ERROR_CODE_KINDS[code];
}

const KNOWN_CODES: ReadonlySet<string> = new Set(API_ERROR_CODES);

export function isApiErrorCode(value: unknown): value is ApiErrorCode {
  return typeof value === 'string' && KNOWN_CODES.has(value);
}
