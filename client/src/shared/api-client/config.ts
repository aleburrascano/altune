const DEFAULT_BASE = 'http://127.0.0.1:8000';

export const REQUEST_TIMEOUT_MS = 15_000;

export const LONG_REQUEST_TIMEOUT_MS = 90_000;

const API_URL_VAR = 'EXPO_PUBLIC_API_URL';

const API_BASE_SHAPE = /^https?:\/\/[^\s/?#]+(\/[^\s?#]*[^\s/?#])?$/;

const INSECURE_SCHEME = 'http://';

const ON_DEVICE_AUTHORITY = /^(localhost|127\.0\.0\.1|\[::1\])(:\d+)?(\/|$)/;

function sendsPlaintextOffDevice(value: string): boolean {
  if (!value.startsWith(INSECURE_SCHEME)) return false;
  return !ON_DEVICE_AUTHORITY.test(value.slice(INSECURE_SCHEME.length));
}

function missingApiBase(isDev: boolean): string {
  if (isDev) return DEFAULT_BASE;
  throw new Error(`Missing required environment variable ${API_URL_VAR}`);
}

function assertWellFormed(value: string): void {
  if (API_BASE_SHAPE.test(value)) return;
  throw new Error(`Invalid ${API_URL_VAR} "${value}": expected http(s)://host[:port][/path]`);
}

function assertSecure(value: string, isDev: boolean): void {
  if (isDev || !sendsPlaintextOffDevice(value)) return;
  throw new Error(
    `Insecure ${API_URL_VAR} "${value}": a release build requires https:// for a remote host`,
  );
}

function resolveApiBase(value: string | undefined, isDev: boolean): string {
  if (value == null || value === '') return missingApiBase(isDev);
  assertWellFormed(value);
  assertSecure(value, isDev);
  return value;
}

export const apiBase = resolveApiBase(process.env.EXPO_PUBLIC_API_URL, __DEV__);
