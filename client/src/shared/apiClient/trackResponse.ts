import type { TrackId } from '@shared/apiClient/ids';

export const ACQUISITION_STATUSES = ['pending', 'ready', 'failed'] as const;
export type AcquisitionStatus = (typeof ACQUISITION_STATUSES)[number];

export type FeaturedArtist = {
  name: string;
  mbid: string | null;
  deezer_id: number | null;
};

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
