import { ContractError } from './errors';
import { apiFetch } from './index';
import { idPathSegment, type TrackId } from './ids';
import type { LibrarySort } from './library';
import { asNumber, asRecord, parseListTracksResponse, parseTrackResponse } from './parse';
import { withQuery } from './queryString';
import type {
  CreateTrackRequest,
  FeaturedArtist,
  ListTracksResponse,
  TrackResponse,
} from './types';

export async function getTracks(
  params: {
    limit: number;
    offset: number;
    q?: string;
    sort?: LibrarySort;
  },
  signal?: AbortSignal,
): Promise<ListTracksResponse> {
  const qs = new URLSearchParams({
    limit: String(params.limit),
    offset: String(params.offset),
  });
  if (params.q) qs.set('q', params.q);
  if (params.sort) qs.set('sort', params.sort);
  return parseListTracksResponse(
    await apiFetch<unknown>(withQuery('/v1/tracks', qs), signal ? { signal } : undefined),
  );
}

const MAX_PAGE = 2000;

// Upper bound on a whole-library fetch (shuffle/play all), so a huge library — or a
// server that never clears has_more — costs at most MAX_ALL_TRACKS / MAX_PAGE requests
// instead of paging without end. Past it the queue is seeded from the first slice.
export const MAX_ALL_TRACKS = 10_000;

export async function getAllTracks(params: {
  q?: string;
  sort?: LibrarySort;
}): Promise<TrackResponse[]> {
  const items: TrackResponse[] = [];
  while (items.length < MAX_ALL_TRACKS) {
    const page = await getTracks({ ...params, limit: MAX_PAGE, offset: items.length });
    items.push(...page.items);
    if (!page.has_more || page.items.length === 0) return items;
  }
  console.warn('[library] whole-library fetch hit its cap; truncating', { cap: MAX_ALL_TRACKS });
  return items.slice(0, MAX_ALL_TRACKS);
}

// makeIdempotencyKey mints a fresh UUID v4 to tag one logical save. The server
// collapses two creates carrying the same key — concurrent double-saves or a
// retry after a dropped response — onto a single library row (see #698). Kept
// local to api-client rather than reusing telemetry's makeEventId, which would
// invert the dependency direction (telemetry imports api-client, not vice versa).
export function makeIdempotencyKey(): string {
  return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, (c) => {
    const r = (Math.random() * 16) | 0;
    const v = c === 'x' ? r : (r & 0x3) | 0x8;
    return v.toString(16);
  });
}

export async function createTrack(
  body: CreateTrackRequest,
  idempotencyKey: string = makeIdempotencyKey(),
): Promise<TrackResponse> {
  return parseTrackResponse(
    await apiFetch<unknown>('/v1/tracks', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'Idempotency-Key': idempotencyKey },
      body: JSON.stringify(body),
    }),
  );
}

export async function deleteTrack(trackId: TrackId): Promise<void> {
  await apiFetch<void>(`/v1/tracks/${idPathSegment(trackId)}`, { method: 'DELETE' });
}

export async function setTrackNumber(trackId: TrackId, trackNumber: number): Promise<void> {
  await apiFetch<void>(`/v1/tracks/${idPathSegment(trackId)}/track-number`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ track_number: trackNumber }),
  });
}

export async function retryAcquisition(trackId: TrackId): Promise<void> {
  await apiFetch<void>(`/v1/tracks/${idPathSegment(trackId)}/retry`, { method: 'POST' });
}

export async function listTracksFeaturing(fa: FeaturedArtist): Promise<ListTracksResponse> {
  const qs = new URLSearchParams();
  if (fa.mbid) qs.set('mbid', fa.mbid);
  if (fa.deezer_id != null) qs.set('deezer_id', String(fa.deezer_id));
  if (fa.name) qs.set('name', fa.name);
  return parseListTracksResponse(await apiFetch<unknown>(withQuery('/v1/tracks/featuring', qs)));
}

export type BackfillFeaturedResult = { scanned: number; updated: number };

function asCount(value: unknown, at: string): number {
  const n = asNumber(value, at);
  if (!Number.isInteger(n) || n < 0) throw new ContractError(at, 'expected a non-negative integer');
  return n;
}

// The counts are interpolated into settings copy ("Updated X of Y tracks"), so an
// off-contract body fails here as a ContractError instead of rendering garbage (#843).
function parseBackfillFeaturedResult(
  value: unknown,
  at = 'BackfillFeaturedResult',
): BackfillFeaturedResult {
  const r = asRecord(value, at);
  const scanned = asCount(r.scanned, `${at}.scanned`);
  const updated = asCount(r.updated, `${at}.updated`);
  if (updated > scanned) throw new ContractError(`${at}.updated`, 'exceeds scanned');
  return { scanned, updated };
}

export async function backfillFeaturedArtists(): Promise<BackfillFeaturedResult> {
  return parseBackfillFeaturedResult(
    await apiFetch<unknown>('/v1/tracks/featured-backfill', { method: 'POST' }),
  );
}

export async function reacquireTrack(trackId: TrackId): Promise<void> {
  await apiFetch<void>(`/v1/tracks/${idPathSegment(trackId)}/reacquire`, { method: 'POST' });
}
