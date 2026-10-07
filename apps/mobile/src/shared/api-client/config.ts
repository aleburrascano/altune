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

function resolveApiBase(value: string | undefined, isDev: boolean): string {
  if (value == null || value === '') {
    if (isDev) return DEFAULT_BASE;
    throw new Error(`Missing required environment variable ${API_URL_VAR}`);
  }
  if (!API_BASE_SHAPE.test(value)) {
    throw new Error(`Invalid ${API_URL_VAR} "${value}": expected http(s)://host[:port][/path]`);
  }
  if (!isDev && sendsPlaintextOffDevice(value)) {
    throw new Error(
      `Insecure ${API_URL_VAR} "${value}": a release build requires https:// for a remote host`,
    );
  }
  return value;
}

export const apiBase = resolveApiBase(process.env.EXPO_PUBLIC_API_URL, __DEV__);
