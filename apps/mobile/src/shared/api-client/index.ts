/**
 * Shared HTTP client — base URL + minimal typed fetch wrapper.
 *
 * Per ADR-0005: feature hooks call typed API client functions (see
 * `./tracks.ts`); React Query wraps the hooks.
 *
 * Per ADR-0006 (auth-integration): apiFetch injects
 * `Authorization: Bearer <access_token>` from the Supabase session
 * UNCONDITIONALLY when a session exists. /health is server-side
 * unauthenticated and accepts/ignores the header — no opt-out needed in v1.
 * If a future endpoint requires a no-auth request, add a `skipAuth` option.
 *
 * EXPO_PUBLIC_API_URL overrides the default base in dev/prod builds:
 *   EXPO_PUBLIC_API_URL=https://altune.example.com npm start
 */
import { supabase } from '../auth/supabaseClient';
import { markSessionExpired } from '../auth/sessionExpired';

const DEFAULT_BASE = 'http://127.0.0.1:8000';

export const apiBase = process.env.EXPO_PUBLIC_API_URL ?? DEFAULT_BASE;

export class ApiError extends Error {
  constructor(
    public readonly status: number,
    message: string,
  ) {
    super(message);
    this.name = 'ApiError';
  }
}

export async function apiFetch<T>(path: string, init?: RequestInit): Promise<T> {
  // AIDEV-NOTE: ngrok-skip-browser-warning header bypasses ngrok free-tier's
  // abuse-prevention interstitial page. Harmless when the URL isn't ngrok.
  // Drop this when we move off ngrok or upgrade to a paid plan.
  const baseHeaders: Record<string, string> = {
    'ngrok-skip-browser-warning': '1',
  };

  // Bearer injection (ADR-0006). The SDK keeps the current session in-memory
  // after restore from secure-store, so getSession is fast and synchronous-
  // looking.
  //
  // AIDEV-NOTE: getSession RESOLVES with `{ session: null, error }` when the
  // refresh token is stale — it does not throw (auth-js GoTrueClient
  // __loadSession). The `error` field must be read explicitly; a try/catch here
  // catches nothing. Every apiFetch path is /v1/* and requires auth, so a
  // missing session means the request is already doomed: fail with a legible
  // reason instead of sending an unauthenticated request and reporting the
  // resulting 401 as if the server had an opinion. (Add a `skipAuth` option
  // here if an unauthenticated endpoint ever needs calling — /health today is
  // not routed through apiFetch.)
  const { data, error } = await supabase.auth.getSession();
  const accessToken = data.session?.access_token;
  if (error != null || accessToken == null) {
    throw new ApiError(401, `API ${path} requires a session: ${error?.message ?? 'no active session'}`);
  }
  baseHeaders.Authorization = `Bearer ${accessToken}`;

  const headers = {
    ...baseHeaders,
    ...(init?.headers ?? {}),
  };
  const response = await fetch(`${apiBase}${path}`, { ...init, headers });
  if (response.status === 401) {
    // The SDK handed us a token the backend rejected — the session is dead
    // server-side but the SDK has no idea. Nothing here acts on it; AuthGate
    // offers the user an explicit re-auth. See shared/auth/sessionExpired.
    markSessionExpired();
  }
  if (!response.ok) {
    throw new ApiError(response.status, `API ${path} returned ${response.status}`);
  }
  if (response.status === 204 || response.status === 304) {
    return undefined as T;
  }
  return (await response.json()) as T;
}
