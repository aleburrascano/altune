import { supabase } from '@shared/auth/supabaseClient';
import { apiBase, apiFetch } from './index';

// A track id as one opaque path segment. `.` and `..` survive encodeURIComponent and URL parsers
// resolve them as dot segments (even as `%2E`), so their dots are double-encoded to reach the
// server as an unknown id instead of a different route.
function trackPathSegment(trackId: string): string {
  const encoded = encodeURIComponent(trackId);
  return /^\.{1,2}$/.test(trackId) ? encoded.replace(/\./g, '%252E') : encoded;
}

export function audioStreamUrl(trackId: string): string {
  return `${apiBase}/v1/tracks/${trackPathSegment(trackId)}/audio`;
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
  version: string;
}

export async function recoverAudio(trackId: string): Promise<void> {
  await apiFetch<undefined>(`/v1/tracks/${trackPathSegment(trackId)}/audio/recover`, {
    method: 'POST',
  });
}

export async function fetchAudioUrls(trackIds: string[]): Promise<ResolvedAudioUrl[]> {
  if (trackIds.length === 0) return [];

  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), 2500);
  try {
    const data = await apiFetch<{ urls: { track_id: string; url: string; version?: string }[] }>(
      '/v1/audio-urls',
      {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ track_ids: trackIds }),
        signal: controller.signal,
      },
    );
    return data.urls.map((u) => ({ trackId: u.track_id, url: u.url, version: u.version ?? '' }));
  } finally {
    clearTimeout(timeout);
  }
}
