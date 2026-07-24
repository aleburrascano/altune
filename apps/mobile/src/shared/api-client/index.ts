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
  const baseHeaders: Record<string, string> = {
    'ngrok-skip-browser-warning': '1',
  };

  const { data, error } = await supabase.auth.getSession();
  const accessToken = data.session?.access_token;
  if (error != null || accessToken == null) {
    throw new ApiError(
      401,
      `API ${path} requires a session: ${error?.message ?? 'no active session'}`,
    );
  }
  baseHeaders.Authorization = `Bearer ${accessToken}`;

  const headers = {
    ...baseHeaders,
    ...(init?.headers ?? {}),
  };
  const response = await fetch(`${apiBase}${path}`, { ...init, headers });
  if (response.status === 401) {
    markSessionExpired();
  }
  if (!response.ok) {
    throw new ApiError(response.status, `API ${path} returned ${response.status}`);
  }
  if (response.status === 202 || response.status === 204 || response.status === 304) {
    return undefined as T;
  }
  return (await response.json()) as T;
}
