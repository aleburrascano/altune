import { Platform } from 'react-native';
import { supabase } from '../auth/supabaseClient';
import { withinAuthDeadline } from '../auth/authDeadline';
import { markSessionExpired, stampCredentials, type CredentialStamp } from '../auth/sessionExpired';
import { CORRELATION_HEADER, newCorrelationId } from './correlationId';
import { startDeadline } from '@shared/deadline/deadline';
import type { Deadline } from '@shared/deadline/deadline';
import {
  ApiError,
  ContractError,
  NetworkError,
  isAbort,
  isSessionFetchFailure,
} from '@shared/errors';
import { parseErrorBody } from './wireDecoders';

export { ApiError, NetworkError, ContractError, isRetryable } from '@shared/errors';
export type { NetworkFailure } from '@shared/errors';

const DEFAULT_BASE = 'http://127.0.0.1:8000';

export const REQUEST_TIMEOUT_MS = 15_000;

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
  route: string,
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
        `API ${route} timed out after ${REQUEST_TIMEOUT_MS}ms`,
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
  if (Platform.OS === 'web') return {};
  return { 'ngrok-skip-browser-warning': '1' };
}

async function requestHeaders(
  path: string,
  correlationId: string | undefined,
  init?: RequestInit,
): Promise<Record<string, string>> {
  return {
    ...tunnelWarningHeader(),
    ...(correlationId === undefined ? {} : { [CORRELATION_HEADER]: correlationId }),
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
  const route = path.split('?', 1)[0] ?? path;
  const sentWith = stampCredentials();
  try {
    const deadline = startDeadline(init?.signal ?? undefined, REQUEST_TIMEOUT_MS);
    try {
      const headers = await requestHeaders(route, correlationId, init);
      return await receive<T>(
        await send(`${apiBase}${path}`, route, { ...init, headers }, deadline, correlationId),
        route,
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

export function signalInit(signal: AbortSignal | undefined): RequestInit | undefined {
  return signal ? { signal } : undefined;
}

type MutationMethod = 'POST' | 'PUT' | 'PATCH' | 'DELETE';

type MutationInit = {
  headers?: Record<string, string>;
  signal?: AbortSignal;
};

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
