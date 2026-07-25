import { supabase } from '../auth/supabaseClient';
import { markSessionExpired } from '../auth/sessionExpired';
import { startDeadline } from './deadline';
import type { Deadline } from './deadline';
import { ApiError, NetworkError, isAbort, isSessionFetchFailure } from './errors';

export { ApiError, NetworkError, isRetryable } from './errors';
export type { NetworkFailure } from './errors';

const DEFAULT_BASE = 'http://127.0.0.1:8000';

export const REQUEST_TIMEOUT_MS = 15_000;

export const apiBase = process.env.EXPO_PUBLIC_API_URL ?? DEFAULT_BASE;

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

export async function apiFetch<T>(path: string, init?: RequestInit): Promise<T> {
  const headers = {
    'ngrok-skip-browser-warning': '1',
    Authorization: await authorization(path),
    ...(init?.headers ?? {}),
  };

  const deadline = startDeadline(init?.signal ?? undefined, REQUEST_TIMEOUT_MS);
  try {
    const response = await send(`${apiBase}${path}`, { ...init, headers }, deadline);
    if (response.status === 401) markSessionExpired();
    if (!response.ok) {
      throw new ApiError(response.status, `API ${path} returned ${response.status}`);
    }
    return await readBody<T>(response, path);
  } finally {
    deadline.release();
  }
}
