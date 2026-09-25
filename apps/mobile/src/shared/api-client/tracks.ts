import * as Crypto from 'expo-crypto';

import { ContractError } from '@shared/errors';
import { apiFetch, apiSend, signalInit } from './index';
import { asTrackId, idPathSegment, type TrackId } from './ids';
import type { LibrarySort } from './library';
import { withQuery } from './queryString';
import type {
  AcquisitionStatus,
  CreateTrackRequest,
  FeaturedArtist,
  ListTracksResponse,
  TrackAcquisition,
  TrackResponse,
} from './types';
import {
  asCount,
  parseArray,
  parseListEnvelope,
  asBoolean,
  asNumber,
  asRecord,
  asString,
  member,
  nullableNumber,
  nullableString,
} from './wireDecoders';

const ACQUISITION_STATUSES = ['pending', 'ready', 'failed'] as const;

function parseFeaturedArtist(value: unknown, at: string): FeaturedArtist {
  const r = asRecord(value, at);
  return {
    name: asString(r.name, `${at}.name`),
    mbid: nullableString(r.mbid, `${at}.mbid`),
    deezer_id: nullableNumber(r.deezer_id, `${at}.deezer_id`),
  };
}

// Only a failed track carries failure text. The Go DTO already sends
// failure_message solely for `failed` and nulls failure_reason on every other
// transition, so dropping them here for pending/ready loses nothing real; it
// keeps the decoded value inside the TrackAcquisition union. failure_message
// stays absent when the wire omits it.
function decodeAcquisition(
  r: Record<string, unknown>,
  at: string,
  status: AcquisitionStatus,
  n: TrackNarrowers,
): TrackAcquisition {
  if (status !== 'failed') return { acquisition_status: status, failure_reason: null };
  return {
    acquisition_status: status,
    failure_reason: n.nullableString(r.failure_reason, `${at}.failure_reason`),
    ...(r.failure_message !== undefined
      ? { failure_message: n.nullableString(r.failure_message, `${at}.failure_message`) }
      : {}),
  };
}

// The single TrackResponse field list, shared by the strict REST parser and the
// lenient SSE parser. The two differ only in how a wire field is narrowed: strict
// throws on a wrong type, lenient coerces an off-type nullable field to null.
interface TrackNarrowers {
  nullableString: (value: unknown, at: string) => string | null;
  nullableNumber: (value: unknown, at: string) => number | null;
}

const STRICT_NARROWERS: TrackNarrowers = { nullableString, nullableNumber };

const LENIENT_NARROWERS: TrackNarrowers = {
  nullableString: (value) => (typeof value === 'string' ? value : null),
  nullableNumber: (value) => (typeof value === 'number' ? value : null),
};

function buildTrackResponse(
  r: Record<string, unknown>,
  at: string,
  n: TrackNarrowers,
): TrackResponse {
  const status: AcquisitionStatus = member(
    r.acquisition_status,
    ACQUISITION_STATUSES,
    `${at}.acquisition_status`,
  );
  return {
    id: asTrackId(asString(r.id, `${at}.id`)),
    title: asString(r.title, `${at}.title`),
    artist: asString(r.artist, `${at}.artist`),
    album: n.nullableString(r.album, `${at}.album`),
    duration_seconds: n.nullableNumber(r.duration_seconds, `${at}.duration_seconds`),
    added_at: asString(r.added_at, `${at}.added_at`),
    artwork_url: n.nullableString(r.artwork_url, `${at}.artwork_url`),
    year: n.nullableNumber(r.year, `${at}.year`),
    genre: n.nullableString(r.genre, `${at}.genre`),
    track_number: n.nullableNumber(r.track_number, `${at}.track_number`),
    album_artist: n.nullableString(r.album_artist, `${at}.album_artist`),
    isrc: n.nullableString(r.isrc, `${at}.isrc`),
    audio_ref: n.nullableString(r.audio_ref, `${at}.audio_ref`),
    ...decodeAcquisition(r, at, status, n),
    ...(r.featured_artists !== undefined
      ? {
          featured_artists: parseArray(
            r.featured_artists,
            `${at}.featured_artists`,
            parseFeaturedArtist,
          ),
        }
      : {}),
  };
}

export function parseTrackResponse(value: unknown, at = 'TrackResponse'): TrackResponse {
  return buildTrackResponse(asRecord(value, at), at, STRICT_NARROWERS);
}

// Lenient sibling for the SSE path: a required field with the wrong type (or an
// off-contract acquisition_status) yields null so the caller skips the upsert,
// while an off-type nullable field is coerced to null rather than rejected.
export function tryParseTrackResponse(value: unknown, at = 'TrackResponse'): TrackResponse | null {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) return null;
  try {
    return buildTrackResponse(value as Record<string, unknown>, at, LENIENT_NARROWERS);
  } catch {
    return null;
  }
}

export function parseListTracksResponse(
  value: unknown,
  at = 'ListTracksResponse',
): ListTracksResponse {
  const r = asRecord(value, at);
  return {
    ...parseListEnvelope(r, at, parseTrackResponse),
    limit: asNumber(r.limit, `${at}.limit`),
    offset: asNumber(r.offset, `${at}.offset`),
    has_more: asBoolean(r.has_more, `${at}.has_more`),
  };
}

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
    await apiFetch<unknown>(withQuery('/v1/tracks', qs), signalInit(signal)),
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
// retry after a dropped response — onto a single library row (see #698), so a
// repeat here silently discards a genuinely distinct save. That safety is the
// v4's 122 bits of collision resistance, which only hold for independent draws:
// Math.random's state is recoverable from earlier keys, so the draws were never
// independent (#1774). Kept local to api-client rather than reusing telemetry's
// makeEventId, which would invert the dependency direction (telemetry imports
// api-client, not vice versa).
export function makeIdempotencyKey(): string {
  return Crypto.randomUUID();
}

export async function createTrack(
  body: CreateTrackRequest,
  idempotencyKey: string = makeIdempotencyKey(),
): Promise<TrackResponse> {
  return parseTrackResponse(
    await apiSend<unknown>('/v1/tracks', 'POST', body, {
      headers: { 'Idempotency-Key': idempotencyKey },
    }),
  );
}

export async function deleteTrack(trackId: TrackId): Promise<void> {
  await apiFetch<void>(`/v1/tracks/${idPathSegment(trackId)}`, { method: 'DELETE' });
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
