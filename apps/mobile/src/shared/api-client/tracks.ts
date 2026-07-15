/**
 * Typed client for the catalog endpoints.
 */

import { apiFetch } from './index';
import type { CreateTrackRequest, FeaturedArtist, ListTracksResponse, TrackResponse } from './types';

export async function getTracks(params: {
  limit: number;
  offset: number;
}): Promise<ListTracksResponse> {
  const qs = new URLSearchParams({
    limit: String(params.limit),
    offset: String(params.offset),
  });
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

/**
 * Persist a track's album position (fill-only on the server — never overwrites an
 * existing value). Used to write back positions the album detail derived from the
 * album tracklist for tracks saved before track_number was captured.
 */
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

/** "Everything featuring X" — the user's saved tracks crediting a featured artist,
 * identified by mbid / deezer_id / name (precedence server-side). */
export async function listTracksFeaturing(fa: FeaturedArtist): Promise<ListTracksResponse> {
  const qs = new URLSearchParams();
  if (fa.mbid) qs.set('mbid', fa.mbid);
  if (fa.deezer_id != null) qs.set('deezer_id', String(fa.deezer_id));
  if (fa.name) qs.set('name', fa.name);
  return apiFetch<ListTracksResponse>(`/v1/tracks/featuring?${qs.toString()}`);
}

export type BackfillFeaturedResult = { scanned: number; updated: number };

/** Trigger the featured-artist backfill over the authed user's existing library. */
export async function backfillFeaturedArtists(): Promise<BackfillFeaturedResult> {
  return apiFetch<BackfillFeaturedResult>('/v1/tracks/featured-backfill', { method: 'POST' });
}
