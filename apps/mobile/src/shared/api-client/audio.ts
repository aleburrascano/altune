import { supabase } from '@shared/auth/supabaseClient';
import { idPathSegment, type TrackId } from './ids';
import { apiBase, apiFetch } from './index';

export function audioStreamUrl(trackId: TrackId): string {
  return `${apiBase}/v1/tracks/${idPathSegment(trackId)}/audio`;
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

export async function recoverAudio(trackId: TrackId): Promise<void> {
  await apiFetch<undefined>(`/v1/tracks/${idPathSegment(trackId)}/audio/recover`, {
    method: 'POST',
  });
}

// Remote kill switch for audio prefetching (#824). The server reports its AUDIO_PREFETCH_ENABLED
// setting on every audio-url response; the last value seen wins, so flipping it on the API
// disables (or restores) prefetching in shipped builds without a release. A server that does not
// send the flag leaves prefetching enabled.
let prefetchEnabled = true;

export function isAudioPrefetchEnabled(): boolean {
  return prefetchEnabled;
}

export async function fetchAudioUrls(trackIds: string[]): Promise<ResolvedAudioUrl[]> {
  if (trackIds.length === 0) return [];

  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), 2500);
  try {
    const data = await apiFetch<{
      urls: { track_id: string; url: string; version?: string }[];
      prefetch_enabled?: unknown;
    }>('/v1/audio-urls', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ track_ids: trackIds }),
      signal: controller.signal,
    });
    if (typeof data.prefetch_enabled === 'boolean') prefetchEnabled = data.prefetch_enabled;
    return data.urls.map((u) => ({ trackId: u.track_id, url: u.url, version: u.version ?? '' }));
  } finally {
    clearTimeout(timeout);
  }
}
