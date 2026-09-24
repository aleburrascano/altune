import { supabase } from '../auth/supabaseClient';
import { markSessionExpired, stampCredentials, type CredentialStamp } from '../auth/sessionExpired';
import { CORRELATION_HEADER, newCorrelationId } from './correlationId';
import { startDeadline } from './deadline';
import type { Deadline } from './deadline';
import { ApiError, ContractError, NetworkError, isAbort, isSessionFetchFailure } from '@shared/errors';
import { parseErrorBody } from './wireDecoders';

export { ApiError, NetworkError, ContractError, isRetryable } from '@shared/errors';
export type { NetworkFailure } from '@shared/errors';

const DEFAULT_BASE = 'http://127.0.0.1:8000';

export const REQUEST_TIMEOUT_MS = 15_000;

const API_URL_VAR = 'EXPO_PUBLIC_API_URL';

// Scheme and host, optional port and path prefix; no trailing slash (paths
// start with `/`), whitespace, query or fragment.
const API_BASE_SHAPE = /^https?:\/\/[^\s/?#]+(\/[^\s?#]*[^\s/?#])?$/;

const INSECURE_SCHEME = 'http://';

// The whole authority, anchored at both ends, so a lookalike that merely
// begins with a loopback host (`127.0.0.1.example.org`, `127.0.0.1@evil.org`)
// is read as the remote host it really is. Anything else fails closed.
const ON_DEVICE_AUTHORITY = /^(localhost|127\.0\.0\.1|\[::1\])(:\d+)?(\/|$)/;

function sendsPlaintextOffDevice(value: string): boolean {
  if (!value.startsWith(INSECURE_SCHEME)) return false;
  return !ON_DEVICE_AUTHORITY.test(value.slice(INSECURE_SCHEME.length));
}

/**
 * Validated once at module load, like the Supabase env vars, so a build that
 * was never given an API URL fails loudly at startup instead of surfacing as a
 * generic network error on every screen. Only a development build may fall
 * back to the loopback default or talk plaintext to a remote host; a malformed
 * value is rejected in every build.
 *
 * Every request built from the result carries the session's bearer token, so a
 * release build reaching a remote host over `http://` would put that token and
 * the event stream on the wire in the clear.
 */
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

/**
 * Refuses rather than returning an empty header, so no request ever leaves
 * unauthenticated. A caller outside `apiFetch` must keep the two refusals
 * apart: `ApiError(401)` is a session that is gone or rejected, `NetworkError`
 * is the auth server itself being unreachable, which must not expire a session.
 */
export async function authorization(
  path: string,
  correlationId: string | undefined,
): Promise<string> {
  const { data, error } = await supabase.auth.getSession();
  if (isSessionFetchFailure(error)) {
    throw new NetworkError(
      'transport',
      `API ${path} could not reach the auth server`,
      correlationId,
    );
  }
  const accessToken = data.session?.access_token;
  if (error != null || accessToken == null) {
    throw new ApiError(
      401,
      `API ${path} requires a session: ${error?.message ?? 'no active session'}`,
      undefined,
      correlationId,
    );
  }
  return `Bearer ${accessToken}`;
}

async function send(
  url: string,
  init: RequestInit,
  deadline: Deadline,
  correlationId: string | undefined,
): Promise<Response> {
  try {
    return await fetch(url, { ...init, signal: deadline.signal });
  } catch (cause) {
    if (deadline.cancelled()) throw cause;
    if (deadline.expired()) {
      throw new NetworkError(
        'timeout',
        `API ${url} timed out after ${REQUEST_TIMEOUT_MS}ms`,
        correlationId,
      );
    }
    if (isAbort(cause)) throw cause;
    throw new NetworkError('transport', `API ${url} is unreachable`, correlationId);
  }
}

async function errorCode(response: Response): Promise<string | undefined> {
  try {
    return parseErrorBody(await response.json()).code;
  } catch {
    return undefined;
  }
}

async function readBody<T>(
  response: Response,
  path: string,
  correlationId: string | undefined,
): Promise<T> {
  if (response.status === 202 || response.status === 204 || response.status === 304) {
    return undefined as T;
  }
  try {
    return (await response.json()) as T;
  } catch {
    throw new NetworkError('transport', `API ${path} returned a truncated response`, correlationId);
  }
}

/**
 * The most a failed request may carry into a log, so redaction lives here
 * rather than at each branch (#1703). Never the caught error's own message: it
 * can hold a server message or a search term, and its stack the local paths.
 * `ContractError.at` is exempt because the decoders build it from literal
 * schema paths, and it is the one field that says which part of the response
 * broke the contract.
 *
 * The last two arms are the catch-all (#1791): before it, an unrecognized
 * throw produced no line at all, so a schema violation was silent in
 * production.
 */
function failureFields(error: unknown): Record<string, string | number> {
  if (error instanceof ApiError) {
    return { status: error.status, ...(error.code === undefined ? {} : { code: error.code }) };
  }
  if (error instanceof NetworkError) return { failure: error.failure };
  if (error instanceof ContractError) return { error: error.name, at: error.at };
  if (error instanceof Error) return { error: error.name };
  return { error: typeof error };
}

/**
 * Leaves a trace of a failed request where it is thrown, so a caller that
 * turns the error into a flag or a closed sheet still leaves evidence. The
 * line carries the pathname without its query string (search terms), never the
 * caller's headers (the bearer token) or the request body; `failureFields`
 * keeps the error itself redacted. An abort is the caller cancelling, not a
 * failure, so it stays unlogged. The correlation id matches the server's log
 * lines.
 *
 * Exported for the one request this client builds but does not send: the native
 * player streams audio over its own HTTP client, and a failure there would
 * otherwise leave no line at all.
 */
export function logFailure(
  method: string,
  path: string,
  correlationId: string | undefined,
  error: unknown,
): void {
  if (isAbort(error)) return;
  console.warn('[api] request failed', {
    method,
    path: path.split('?')[0],
    ...(correlationId === undefined ? {} : { correlationId }),
    ...failureFields(error),
  });
}

/**
 * `Authorization` is spread last, after the caller's headers, so a call site
 * that forwards a header set from another context cannot replace or strip the
 * session's own bearer token. Every other header here stays caller-overridable.
 */
async function requestHeaders(
  path: string,
  correlationId: string | undefined,
  init?: RequestInit,
): Promise<Record<string, string>> {
  return {
    'ngrok-skip-browser-warning': '1',
    ...(correlationId === undefined ? {} : { [CORRELATION_HEADER]: correlationId }),
    // Callers always pass record-shaped headers; the RequestInit type also
    // permits Headers/[][], neither of which is meaningful to spread here.
    ...((init?.headers ?? {}) as Record<string, string>),
    Authorization: await authorization(path, correlationId),
  };
}

async function receive<T>(
  response: Response,
  path: string,
  correlationId: string | undefined,
  sentWith: CredentialStamp,
): Promise<T> {
  if (response.status === 401) markSessionExpired(sentWith);
  if (!response.ok) {
    throw new ApiError(
      response.status,
      `API ${path} returned ${response.status}`,
      await errorCode(response),
      correlationId,
    );
  }
  return readBody<T>(response, path, correlationId);
}

export async function apiFetch<T>(path: string, init?: RequestInit): Promise<T> {
  const correlationId = newCorrelationId() ?? undefined;
  const sentWith = stampCredentials();
  try {
    const headers = await requestHeaders(path, correlationId, init);
    const deadline = startDeadline(init?.signal ?? undefined, REQUEST_TIMEOUT_MS);
    try {
      return await receive<T>(
        await send(`${apiBase}${path}`, { ...init, headers }, deadline, correlationId),
        path,
        correlationId,
        sentWith,
      );
    } finally {
      deadline.release();
    }
  } catch (error) {
    logFailure((init?.method ?? 'GET').toUpperCase(), path, correlationId, error);
    throw error;
  }
}

type MutationMethod = 'POST' | 'PUT' | 'PATCH' | 'DELETE';

// Everything a JSON mutation varies beyond its path, method and body: an
// `Idempotency-Key` where the caller mints one, an abort signal where the call
// carries a deadline of its own.
type MutationInit = {
  headers?: Record<string, string>;
  signal?: AbortSignal;
};

/**
 * The one way to send a JSON body, so a new endpoint cannot ship with a
 * mistyped content type or a body that was never serialized. `Content-Type` is
 * spread last for the reason `Authorization` is in `requestHeaders`: this is
 * the call that serialized the body, so a caller's header set cannot re-label
 * it as something the payload is not.
 */
export function apiSend<T>(
  path: string,
  method: MutationMethod,
  body: unknown,
  init?: MutationInit,
): Promise<T> {
  return apiFetch<T>(path, {
    ...init,
    method,
    headers: { ...init?.headers, 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
}
