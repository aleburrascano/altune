import type { PlaylistId, TrackId } from '@shared/apiClient/ids';
import type { TrackResponse } from '@shared/apiClient/trackResponse';

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
