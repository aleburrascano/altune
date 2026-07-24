/**
 * Lyrics — Deezer-backed, tracks only (`GET /v1/discovery/lyrics`).
 *
 * Identified by `title` + `subtitle` (artist), the same kind-less pair the other
 * enrichment endpoints use. The server always answers 200: an unresolved track
 * returns an empty DTO rather than a 404, so callers branch on emptiness, never
 * on an error.
 */
import { apiFetch } from './index';

/** One time-synced lyric line. `milliseconds` is the line's start offset into
 *  the track; `duration` is how long it holds (0 when the provider omits it). */
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
  // Go's `omitempty` drops empty slices from the wire, so an absent list arrives
  // as undefined despite the declared type. Coerce at the boundary.
  return {
    ...response,
    synced_lines: response.synced_lines ?? [],
    writers: response.writers ?? [],
  };
}
