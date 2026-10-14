import { isWebPlatform, platformName, telemetryVersion } from '@shared/device/device';
import { shareAnalytics } from '@shared/device/analyticsPreference';
import { supabase } from '@shared/auth/supabaseClient';
import { withinAuthDeadline } from '@shared/auth/authDeadline';
import {
  markSessionExpired,
  stampCredentials,
  type CredentialStamp,
} from '@shared/auth/sessionExpired';
import { CORRELATION_HEADER, newCorrelationId } from '@shared/apiClient/correlationId';
import { startDeadline } from '@shared/timing/deadline';
import type { Deadline } from '@shared/timing/deadline';
import {
  ApiError,
  ContractError,
  HTTP_UNAUTHORIZED,
  NetworkError,
  isAbort,
  isSessionFetchFailure,
} from '@shared/failure/errors';
import { parseErrorBody } from '@shared/apiClient/wireDecoders';
import { apiBase, REQUEST_TIMEOUT_MS } from '@shared/apiClient/config';

export { ApiError, NetworkError } from '@shared/failure/errors';

export { apiBase, LONG_REQUEST_TIMEOUT_MS } from '@shared/apiClient/config';

type CorrelationId = string | undefined;

declare global {
  interface RequestInit {
    timeoutMs?: number;
  }
}

export async function authorization(
  path: string,
  correlationId: string | undefined,
): Promise<string> {
  const { data: stored, error } = await withinAuthDeadline(
    supabase.auth.getSession(),
    `API ${path} auth lookup`,
    correlationId,
  );
  if (isSessionFetchFailure(error)) {
    throw new NetworkError(
      'transport',
      `API ${path} could not reach the auth server`,
      correlationId,
    );
  }
  const accessToken = stored.session?.access_token;
  if (error != null || accessToken == null) {
    throw new ApiError(
      HTTP_UNAUTHORIZED,
      `API ${path} requires a session: ${error?.message ?? 'no active session'}`,
      undefined,
      correlationId,
    );
  }
  return `Bearer ${accessToken}`;
}

async function send(
  url: string,
  route: string,
  init: RequestInit,
  deadline: Deadline,
  timeoutMs: number,
  correlationId: string | undefined,
): Promise<Response> {
  try {
    return await fetch(url, { ...init, signal: deadline.signal });
  } catch (cause) {
    if (deadline.cancelled()) throw cause;
    if (deadline.expired()) {
      throw new NetworkError(
        'timeout',
        `API ${route} timed out after ${timeoutMs}ms`,
        correlationId,
      );
    }
    if (isAbort(cause)) throw cause;
    throw new NetworkError('transport', `API ${route} is unreachable`, correlationId);
  }
}

async function errorCode(response: Response): Promise<string | undefined> {
  try {
    return parseErrorBody(await response.json()).code;
  } catch {
    return undefined;
  }
}

const HTTP_ACCEPTED = 202;
const HTTP_NO_CONTENT = 204;
const HTTP_NOT_MODIFIED = 304;

const BODYLESS_STATUSES: ReadonlySet<number> = new Set([
  HTTP_ACCEPTED,
  HTTP_NO_CONTENT,
  HTTP_NOT_MODIFIED,
]);

function truncated(path: string, id: CorrelationId): NetworkError {
  return new NetworkError('transport', `API ${path} returned a truncated response`, id);
}

async function readBody(response: Response, path: string, id: CorrelationId): Promise<unknown> {
  if (BODYLESS_STATUSES.has(response.status)) return undefined;
  try {
    return await response.json();
  } catch {
    throw truncated(path, id);
  }
}

function failureFields(error: unknown): Record<string, string | number> {
  if (error instanceof ApiError) {
    return { status: error.status, ...(error.code === undefined ? {} : { code: error.code }) };
  }
  if (error instanceof NetworkError) return { failure: error.failure };
  if (error instanceof ContractError) return { error: error.name, at: error.at };
  if (error instanceof Error) return { error: error.name };
  return { error: typeof error };
}

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

function tunnelWarningHeader(): Record<string, string> {
  if (isWebPlatform()) return {};
  return { 'ngrok-skip-browser-warning': '1' };
}

function analyticsHeader(): Record<string, string> {
  if (shareAnalytics()) return {};
  return { 'X-Altune-Analytics': 'off' };
}

async function requestHeaders(
  path: string,
  correlationId: string | undefined,
  init?: RequestInit,
): Promise<Record<string, string>> {
  return {
    ...tunnelWarningHeader(),
    ...(correlationId === undefined ? {} : { [CORRELATION_HEADER]: correlationId }),
    'X-Altune-App-Version': telemetryVersion(),
    'X-Altune-Platform': platformName(),
    ...analyticsHeader(),
    ...((init?.headers ?? {}) as Record<string, string>),
    Authorization: await authorization(path, correlationId),
  };
}

async function apiError(response: Response, path: string, id: CorrelationId): Promise<ApiError> {
  const code = await errorCode(response);
  return new ApiError(response.status, `API ${path} returned ${response.status}`, code, id);
}

async function receive(
  response: Response,
  path: string,
  id: CorrelationId,
  sentWith: CredentialStamp,
): Promise<unknown> {
  if (response.status === HTTP_UNAUTHORIZED) markSessionExpired(sentWith);
  if (!response.ok) throw await apiError(response, path, id);
  return readBody(response, path, id);
}

const QUERY_SUFFIX_PATTERN = /\?.*/s;

function routeOf(path: string): string {
  return path.replace(QUERY_SUFFIX_PATTERN, '');
}

function timeoutOf(init: RequestInit | undefined): number {
  return init?.timeoutMs ?? REQUEST_TIMEOUT_MS;
}

async function transmit(
  path: string,
  id: CorrelationId,
  init: RequestInit | undefined,
  deadline: Deadline,
): Promise<Response> {
  const route = routeOf(path);
  const headers = await requestHeaders(route, id, init);
  return send(`${apiBase}${path}`, route, { ...init, headers }, deadline, timeoutOf(init), id);
}

async function dispatch(path: string, id: CorrelationId, init?: RequestInit): Promise<unknown> {
  const sentWith = stampCredentials();
  const deadline = startDeadline(init?.signal ?? undefined, timeoutOf(init));
  try {
    const response = await transmit(path, id, init, deadline);
    return await receive(response, routeOf(path), id, sentWith);
  } finally {
    deadline.release();
  }
}

export async function apiFetch(path: string, init?: RequestInit): Promise<unknown> {
  const id = newCorrelationId() ?? undefined;
  try {
    return await dispatch(path, id, init);
  } catch (error) {
    logFailure((init?.method ?? 'GET').toUpperCase(), path, id, error);
    throw error;
  }
}

export function signalInit(signal: AbortSignal | undefined): RequestInit | undefined {
  return signal ? { signal } : undefined;
}

type MutationMethod = 'POST' | 'PUT' | 'PATCH' | 'DELETE';

type MutationInit = {
  headers?: Record<string, string>;
  signal?: AbortSignal;
};

function jsonRequest(method: MutationMethod, body: unknown, init?: MutationInit): RequestInit {
  return {
    ...init,
    method,
    headers: { ...init?.headers, 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  };
}

export function apiSend(
  path: string,
  method: MutationMethod,
  body: unknown,
  init?: MutationInit,
): Promise<unknown> {
  return apiFetch(path, jsonRequest(method, body, init));
}

export function withQuery(path: string, params: URLSearchParams): string {
  const qs = params.toString();
  return qs ? `${path}?${qs}` : path;
}
