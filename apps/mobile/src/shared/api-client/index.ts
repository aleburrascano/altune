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
  HTTP_UNAUTHORIZED,
  NetworkError,
  isAbort,
  isSessionFetchFailure,
} from '@shared/errors';
import { parseErrorBody } from './wireDecoders';
import { apiBase, REQUEST_TIMEOUT_MS } from './config';

export { ApiError, NetworkError, ContractError } from '@shared/errors';
export type { NetworkFailure } from '@shared/errors';

export { apiBase, REQUEST_TIMEOUT_MS, LONG_REQUEST_TIMEOUT_MS } from './config';

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
  if (response.status === HTTP_UNAUTHORIZED) markSessionExpired(sentWith);
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
  const route = path.replace(/\?.*/s, '');
  const sentWith = stampCredentials();
  const timeoutMs = init?.timeoutMs ?? REQUEST_TIMEOUT_MS;
  try {
    const deadline = startDeadline(init?.signal ?? undefined, timeoutMs);
    try {
      const headers = await requestHeaders(route, correlationId, init);
      return await receive<T>(
        await send(
          `${apiBase}${path}`,
          route,
          { ...init, headers },
          deadline,
          timeoutMs,
          correlationId,
        ),
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
