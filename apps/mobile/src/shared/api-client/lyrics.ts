import { apiFetch } from './index';

export type SyncedLine = {
  timecode: string;
  line: string;
  milliseconds: number;
  duration: number;
};

export type LyricsResponse = {
  plain: string;
  synced_lines: SyncedLine[];
  writers: string[];
  copyright: string;
};

export async function getLyrics(params: {
  title: string;
  subtitle?: string | null | undefined;
}): Promise<LyricsResponse> {
  const qs = new URLSearchParams({ title: params.title });
  if (params.subtitle != null && params.subtitle.length > 0) {
    qs.set('subtitle', params.subtitle);
  }
  const response = await apiFetch<LyricsResponse>(`/v1/discovery/lyrics?${qs.toString()}`);
  return {
    ...response,
    synced_lines: response.synced_lines ?? [],
    writers: response.writers ?? [],
  };
}
