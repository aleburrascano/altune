import { apiFetch } from './index';
import type { LibrarySort } from './library';
import type {
  CreateTrackRequest,
  FeaturedArtist,
  ListTracksResponse,
  TrackResponse,
} from './types';

export async function getTracks(params: {
  limit: number;
  offset: number;
  q?: string;
  sort?: LibrarySort;
}): Promise<ListTracksResponse> {
  const qs = new URLSearchParams({
    limit: String(params.limit),
    offset: String(params.offset),
  });
  if (params.q) qs.set('q', params.q);
  if (params.sort) qs.set('sort', params.sort);
  return apiFetch<ListTracksResponse>(`/v1/tracks?${qs.toString()}`);
}

export async function createTrack(body: CreateTrackRequest): Promise<TrackResponse> {
  return apiFetch<TrackResponse>('/v1/tracks', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
}

export async function deleteTrack(trackId: string): Promise<void> {
  await apiFetch<void>(`/v1/tracks/${trackId}`, { method: 'DELETE' });
}

export async function setTrackNumber(trackId: string, trackNumber: number): Promise<void> {
  await apiFetch<void>(`/v1/tracks/${trackId}/track-number`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ track_number: trackNumber }),
  });
}

export async function retryAcquisition(trackId: string): Promise<void> {
  await apiFetch<void>(`/v1/tracks/${trackId}/retry`, { method: 'POST' });
}

export async function listTracksFeaturing(fa: FeaturedArtist): Promise<ListTracksResponse> {
  const qs = new URLSearchParams();
  if (fa.mbid) qs.set('mbid', fa.mbid);
  if (fa.deezer_id != null) qs.set('deezer_id', String(fa.deezer_id));
  if (fa.name) qs.set('name', fa.name);
  return apiFetch<ListTracksResponse>(`/v1/tracks/featuring?${qs.toString()}`);
}

export type BackfillFeaturedResult = { scanned: number; updated: number };

export async function backfillFeaturedArtists(): Promise<BackfillFeaturedResult> {
  return apiFetch<BackfillFeaturedResult>('/v1/tracks/featured-backfill', { method: 'POST' });
}
