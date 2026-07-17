import { supabase } from '@shared/auth/supabaseClient';
import { apiBase, apiFetch } from '@shared/api-client';

export function audioStreamUrl(trackId: string): string {
  return `${apiBase}/v1/tracks/${trackId}/audio`;
}

// For the NATIVE PLAYER's requests only — the player streams the proxy URL
// itself, outside fetch, so apiFetch can't carry the bearer for it. Every
// JSON /v1 call below goes through apiFetch (the single choke point: bearer
// injection + markSessionExpired on a server 401 — see shared/auth).
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

// Ask the server to reconcile a library track after a playback failure. Presigned
// streams hit storage directly and bypass the proxy's missing-file recovery, so
// this restores it: the server marks a genuinely-gone file failed and schedules
// re-acquisition (a no-op when the file is actually present). Fire-and-forget.
export async function recoverAudio(trackId: string): Promise<void> {
  await apiFetch<undefined>(`/v1/tracks/${trackId}/audio/recover`, { method: 'POST' });
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
    const data = await apiFetch<{ urls: { track_id: string; url: string }[] }>('/v1/audio-urls', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ track_ids: trackIds }),
      signal: controller.signal,
    });
    return data.urls.map((u) => ({ trackId: u.track_id, url: u.url }));
  } finally {
    clearTimeout(timeout);
  }
}
