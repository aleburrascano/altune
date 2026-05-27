/**
 * Shared HTTP client — base URL + minimal typed fetch wrapper.
 *
 * Per ADR-0005: feature hooks call typed API client functions (see
 * `./tracks.ts`); React Query wraps the hooks. The wrapper is intentionally
 * minimal — no auth, no retry, no error mapping beyond surfacing non-2xx
 * as an `ApiError`. Those concerns land via future ADRs (auth) and
 * React Query (retry).
 *
 * EXPO_PUBLIC_API_URL overrides the default base in dev/prod builds:
 *   EXPO_PUBLIC_API_URL=https://altune.example.com npm start
 */

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
  const headers = {
    'ngrok-skip-browser-warning': '1',
    ...(init?.headers ?? {}),
  };
  const response = await fetch(`${apiBase}${path}`, { ...init, headers });
  if (!response.ok) {
    throw new ApiError(response.status, `API ${path} returned ${response.status}`);
  }
  return (await response.json()) as T;
}
