/**
 * Mirrored types for the FastAPI catalog endpoints.
 *
 * Hand-maintained for v1; the future OpenAPI codegen spec will replace this
 * with generated types. The mobile and backend shapes must stay in sync —
 * the spec's Risk #2 calls this out, and the plan-reviewer's grep is the
 * mitigation.
 */

export type TrackResponse = {
  id: string; // UUID string from the wire
  title: string;
  artist: string;
  album: string | null;
  duration_seconds: number | null;
  added_at: string; // ISO-8601
  acquisition_status: string; // 'pending' | 'ready' (AcquisitionStatus, wire-lowercase)
  artwork_url: string | null;
  year: number | null;
  genre: string | null;
  track_number: number | null;
  album_artist: string | null;
  isrc: string | null;
  audio_ref: string | null;
};

export type CreateTrackRequest = {
  title: string;
  artist: string;
  album: string | null;
  duration_seconds: number | null;
  artwork_url: string | null;
};

export type ListTracksResponse = {
  items: TrackResponse[];
  total: number;
  limit: number;
  offset: number;
  has_more: boolean;
};
