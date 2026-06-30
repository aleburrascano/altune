/**
 * Mirrored types for the FastAPI catalog endpoints.
 *
 * Hand-maintained for v1; the future OpenAPI codegen spec will replace this
 * with generated types. The mobile and backend shapes must stay in sync —
 * the spec's Risk #2 calls this out, and the plan-reviewer's grep is the
 * mitigation.
 */

export type AcquisitionStatus = 'pending' | 'ready' | 'failed';

export type TrackResponse = {
  id: string; // UUID string from the wire
  title: string;
  artist: string;
  album: string | null;
  duration_seconds: number | null;
  added_at: string; // ISO-8601
  acquisition_status: AcquisitionStatus;
  artwork_url: string | null;
  failure_reason: string | null;
  year: number | null;
  genre: string | null;
  track_number: number | null;
  album_artist: string | null;
  isrc: string | null;
  audio_ref: string | null;
  // Client-populated from `track_acquisition_progress` SSE events (not sent by
  // the list endpoints). The current pipeline stage while acquisition_status is
  // 'pending': search|select|download|tag|store|update_track.
  acquisition_stage?: string;
};

export type CreateTrackRequest = {
  title: string;
  artist: string;
  album: string | null;
  duration_seconds: number | null;
  artwork_url: string | null;
  isrc: string | null;
  year: number | null;
  genre: string | null;
  album_artist: string | null;
  /** Exact provider URL the result was discovered at (e.g. a SoundCloud
   * permalink). When it's a directly-downloadable source the backend acquires
   * that exact track instead of re-searching. Optional; not persisted. */
  source_url?: string | null;
};

export type ListTracksResponse = {
  items: TrackResponse[];
  total: number;
  limit: number;
  offset: number;
  has_more: boolean;
};

export type PlaylistResponse = {
  id: string;
  name: string;
  track_count: number;
  preview_artwork_urls: string[];
  created_at: string;
  updated_at: string;
};

export type ListPlaylistsResponse = {
  items: PlaylistResponse[];
  total: number;
};

export type PlaylistDetailResponse = PlaylistResponse & {
  tracks: TrackResponse[];
};

export type CreatePlaylistRequest = {
  name: string;
};

export type AddTrackToPlaylistRequest = {
  track_id: string;
};

export type ReorderTracksRequest = {
  track_ids: string[];
};
