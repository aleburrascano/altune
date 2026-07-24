import { supabase } from '@shared/auth/supabaseClient';
import { apiBase, apiFetch } from './index';

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

export async function recoverAudio(trackId: string): Promise<void> {
  await apiFetch<undefined>(`/v1/tracks/${trackId}/audio/recover`, { method: 'POST' });
}

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
