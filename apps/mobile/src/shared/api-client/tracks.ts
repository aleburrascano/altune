/**
 * Typed client for the catalog endpoints.
 */

import { apiFetch } from './index';
import type { CreateTrackRequest, ListTracksResponse, TrackResponse } from './types';

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
