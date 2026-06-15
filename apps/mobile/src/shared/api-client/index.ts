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
  // looking. If no session, we omit the header entirely (backend returns 401).
  const { data } = await supabase.auth.getSession();
  if (data.session?.access_token) {
    baseHeaders.Authorization = `Bearer ${data.session.access_token}`;
  }

  const headers = {
    ...baseHeaders,
    ...(init?.headers ?? {}),
  };
  const response = await fetch(`${apiBase}${path}`, { ...init, headers });
  if (!response.ok) {
    throw new ApiError(response.status, `API ${path} returned ${response.status}`);
  }
  return (await response.json()) as T;
}
