export type AcquisitionStatus = 'pending' | 'ready' | 'failed';

export type FeaturedArtist = {
  name: string;
  mbid: string | null;
  deezer_id: number | null;
};

export type TrackResponse = {
  id: string;
  title: string;
  artist: string;
  album: string | null;
  duration_seconds: number | null;
  added_at: string;
  acquisition_status: AcquisitionStatus;
  artwork_url: string | null;
  failure_reason: string | null;
  failure_message?: string | null;
  year: number | null;
  genre: string | null;
  track_number: number | null;
  album_artist: string | null;
  isrc: string | null;
  audio_ref: string | null;
  featured_artists?: FeaturedArtist[];
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
  total_duration_seconds: number;
  tracks: TrackResponse[];
};

export type CreatePlaylistRequest = {
  name: string;
};

export type AddTracksToPlaylistRequest = {
  track_ids: string[];
};

export type AddTracksToPlaylistResponse = {
  added: number;
  skipped: number;
};

export type RemoveTracksFromPlaylistRequest = {
  track_ids: string[];
};

export type RemoveTracksFromPlaylistResponse = {
  removed: number;
};

export type ReorderTracksRequest = {
  track_ids: string[];
};
