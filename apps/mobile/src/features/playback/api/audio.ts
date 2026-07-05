import { supabase } from '@shared/auth/supabaseClient';
import { apiBase } from '@shared/api-client';

export function audioStreamUrl(trackId: string): string {
  return `${apiBase}/v1/tracks/${trackId}/audio`;
}

export async function audioRequestHeaders(): Promise<Record<string, string>> {
  const { data } = await supabase.auth.getSession();
  const headers: Record<string, string> = {};
  if (data.session?.access_token) {
    headers.Authorization = `Bearer ${data.session.access_token}`;
  }
  return headers;
}

export interface ResolvedAudioUrl {
  trackId: string;
  url: string;
}

// Mint short-lived, directly-streamable URLs for library tracks so the native
// player streams straight from storage instead of proxying every byte through
// the API. Tracks the server can't sign (local dev / not ready) are absent from
// the response; the caller falls back to the proxy URL for those. Timed out so a
// slow response never blocks queue start.
export async function fetchAudioUrls(trackIds: string[]): Promise<ResolvedAudioUrl[]> {
  if (trackIds.length === 0) return [];

  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), 2500);
  try {
    const headers = await audioRequestHeaders();
    const res = await fetch(`${apiBase}/v1/audio-urls`, {
      method: 'POST',
      headers: { ...headers, 'Content-Type': 'application/json' },
      body: JSON.stringify({ track_ids: trackIds }),
      signal: controller.signal,
    });
    if (!res.ok) throw new Error(`audio-urls ${res.status}`);
    const data = (await res.json()) as { urls: { track_id: string; url: string }[] };
    return data.urls.map((u) => ({ trackId: u.track_id, url: u.url }));
  } finally {
    clearTimeout(timeout);
  }
}
