import { supabase } from '../auth/supabaseClient';
import { markSessionExpired } from '../auth/sessionExpired';
import { CORRELATION_HEADER, newCorrelationId } from './correlationId';
import { startDeadline } from './deadline';
import type { Deadline } from './deadline';
import { ApiError, NetworkError, isAbort, isSessionFetchFailure } from './errors';
import { parseErrorBody } from './wireDecoders';

export { ApiError, NetworkError, ContractError, isRetryable } from './errors';
export type { NetworkFailure } from './errors';

const DEFAULT_BASE = 'http://127.0.0.1:8000';

export const REQUEST_TIMEOUT_MS = 15_000;

const API_URL_VAR = 'EXPO_PUBLIC_API_URL';

// Scheme and host, optional port and path prefix; no trailing slash (paths
// start with `/`), whitespace, query or fragment.
const API_BASE_SHAPE = /^https?:\/\/[^\s/?#]+(\/[^\s?#]*[^\s/?#])?$/;

/**
 * Validated once at module load, like the Supabase env vars, so a build that
 * was never given an API URL fails loudly at startup instead of surfacing as a
 * generic network error on every screen. Only a development build may fall
 * back to the loopback default; a malformed value is rejected in every build.
 */
function resolveApiBase(value: string | undefined, isDev: boolean): string {
  if (value == null || value === '') {
    if (isDev) return DEFAULT_BASE;
    throw new Error(`Missing required environment variable ${API_URL_VAR}`);
  }
  if (!API_BASE_SHAPE.test(value)) {
    throw new Error(`Invalid ${API_URL_VAR} "${value}": expected http(s)://host[:port][/path]`);
  }
  return value;
}

export const apiBase = resolveApiBase(process.env.EXPO_PUBLIC_API_URL, __DEV__);

async function authorization(path: string): Promise<string> {
  const { data, error } = await supabase.auth.getSession();
  if (isSessionFetchFailure(error)) {
    throw new NetworkError('transport', `API ${path} could not reach the auth server`);
  }
  const accessToken = data.session?.access_token;
  if (error != null || accessToken == null) {
    throw new ApiError(
      401,
      `API ${path} requires a session: ${error?.message ?? 'no active session'}`,
    );
  }
  return `Bearer ${accessToken}`;
}

async function send(url: string, init: RequestInit, deadline: Deadline): Promise<Response> {
  try {
    return await fetch(url, { ...init, signal: deadline.signal });
  } catch (cause) {
    if (deadline.cancelled()) throw cause;
    if (deadline.expired()) {
      throw new NetworkError('timeout', `API ${url} timed out after ${REQUEST_TIMEOUT_MS}ms`);
    }
    if (isAbort(cause)) throw cause;
    throw new NetworkError('transport', `API ${url} is unreachable`);
  }
}

async function errorCode(response: Response): Promise<string | undefined> {
  try {
    return parseErrorBody(await response.json()).code;
  } catch {
    return undefined;
  }
}

async function readBody<T>(response: Response, path: string): Promise<T> {
  if (response.status === 202 || response.status === 204 || response.status === 304) {
    return undefined as T;
  }
  try {
    return (await response.json()) as T;
  } catch {
    throw new NetworkError('transport', `API ${path} returned a truncated response`);
  }
}

/**
 * Leaves a trace of a failed request where it is thrown, so a caller that
 * turns the error into a flag or a closed sheet still leaves evidence. Logs
 * only method, pathname, correlation id, status/code or failure kind: never the
 * query string (search terms), other headers (the bearer token), request body
 * or server message. The correlation id matches the server's log lines.
 */
function logFailure(
  method: string,
  path: string,
  correlationId: string | null,
  error: unknown,
): void {
  const endpoint = {
    method,
    path: path.split('?')[0],
    ...(correlationId === null ? {} : { correlationId }),
  };
  if (error instanceof ApiError) {
    console.warn('[api] request failed', {
      ...endpoint,
      status: error.status,
      ...(error.code === undefined ? {} : { code: error.code }),
    });
  } else if (error instanceof NetworkError) {
    console.warn('[api] request failed', { ...endpoint, failure: error.failure });
  }
}

async function requestHeaders(
  path: string,
  correlationId: string | null,
  init?: RequestInit,
): Promise<Record<string, string>> {
  return {
    'ngrok-skip-browser-warning': '1',
    ...(correlationId === null ? {} : { [CORRELATION_HEADER]: correlationId }),
    Authorization: await authorization(path),
    // Callers always pass record-shaped headers; the RequestInit type also
    // permits Headers/[][], neither of which is meaningful to spread here.
    ...((init?.headers ?? {}) as Record<string, string>),
  };
}

async function receive<T>(response: Response, path: string): Promise<T> {
  if (response.status === 401) markSessionExpired();
  if (!response.ok) {
    throw new ApiError(
      response.status,
      `API ${path} returned ${response.status}`,
      await errorCode(response),
    );
  }
  return readBody<T>(response, path);
}

export async function apiFetch<T>(path: string, init?: RequestInit): Promise<T> {
  const correlationId = newCorrelationId();
  try {
    const headers = await requestHeaders(path, correlationId, init);
    const deadline = startDeadline(init?.signal ?? undefined, REQUEST_TIMEOUT_MS);
    try {
      return await receive<T>(
        await send(`${apiBase}${path}`, { ...init, headers }, deadline),
        path,
      );
    } finally {
      deadline.release();
    }
  } catch (error) {
    logFailure((init?.method ?? 'GET').toUpperCase(), path, correlationId, error);
    throw error;
  }
}
