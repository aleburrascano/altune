import type { PlaylistId, TrackId } from './ids';

export type ApiErrorBody = {
  detail?: string;
  code?: string;
};

export type AcquisitionStatus = 'pending' | 'ready' | 'failed';

export type FeaturedArtist = {
  name: string;
  mbid: string | null;
  deezer_id: number | null;
};

// A track's acquisition state as one value keyed on acquisition_status: only a
// failed track can carry failure text, so a ready or pending track holding a
// stale failure_reason/failure_message is unrepresentable. Build transitions
// with toPending/toReady/toFailed (./trackAcquisition), never a partial patch.
// failure_message is optional because the wire omits it outside `failed`.
export type PendingAcquisition = {
  acquisition_status: 'pending';
  failure_reason: null;
  failure_message?: null;
};

export type ReadyAcquisition = {
  acquisition_status: 'ready';
  failure_reason: null;
  failure_message?: null;
};

export type FailedAcquisition = {
  acquisition_status: 'failed';
  failure_reason: string | null;
  failure_message?: string | null;
};

export type TrackAcquisition = PendingAcquisition | ReadyAcquisition | FailedAcquisition;

export type TrackFields = {
  id: TrackId;
  title: string;
  artist: string;
  album: string | null;
  duration_seconds: number | null;
  added_at: string;
  artwork_url: string | null;
  year: number | null;
  genre: string | null;
  track_number: number | null;
  album_artist: string | null;
  isrc: string | null;
  // Client-only: the Go TrackDTO never serializes its storage key (json:"-",
  // #1046). The cache learns it only from the track_acquisition_completed SSE
  // event; a track decoded from a REST response carries null.
  audio_ref?: string | null;
  featured_artists?: FeaturedArtist[];
};

export type TrackResponse = TrackFields & TrackAcquisition;

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
  track_number: number | null;
  featured_artists?: FeaturedArtist[];
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
  id: PlaylistId;
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
  total_duration_seconds: number;
  tracks: TrackResponse[];
};

export type CreatePlaylistRequest = {
  name: string;
};

export type AddTracksToPlaylistRequest = {
  track_ids: TrackId[];
};

export type AddTracksToPlaylistResponse = {
  added: number;
  skipped: number;
};

export type RemoveTracksFromPlaylistRequest = {
  track_ids: TrackId[];
};

export type RemoveTracksFromPlaylistResponse = {
  removed: number;
};
