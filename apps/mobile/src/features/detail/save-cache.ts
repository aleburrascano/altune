import { trackIdentityKey } from '@shared/acquisition/trackStatusStore';
import { asTrackId, type TrackId } from '@shared/api-client/ids';
import type { CreateTrackRequest, TrackResponse } from '@shared/api-client/types';
import type { DiscoveryResult } from '@shared/api-client/discovery';

import { trackExtras } from './extras-accessors';

export function toCreateTrackRequest(result: DiscoveryResult): CreateTrackRequest {
  const te = trackExtras(result.extras);
  const soundcloudUrl = result.sources.find((s) => s.provider === 'soundcloud')?.url ?? null;
  return {
    title: result.title,
    artist: result.subtitle ?? '',
    album: te.album,
    duration_seconds: te.durationSeconds != null ? Math.floor(te.durationSeconds) : null,
    artwork_url: result.image_url,
    isrc: te.isrc,
    year: te.year,
    genre: te.genre,
    album_artist: te.albumArtist,
    track_number: te.trackPosition,
    ...(te.featuredArtists.length > 0 ? { featured_artists: te.featuredArtists } : {}),
    source_url: soundcloudUrl,
  };
}

const FNV_OFFSET_BASIS = 0x811c9dc5;
const FNV_PRIME = 0x01000193;

// FNV-1a over code points. `basis` is the starting hash: a second pass from a different
// basis yields a second, independent 32-bit digest of the same input.
function fnv1aHex(input: string, basis: number): string {
  let hash = basis;
  for (const ch of input) {
    hash ^= ch.codePointAt(0) ?? 0;
    hash = Math.imul(hash, FNV_PRIME) >>> 0;
  }
  return hash.toString(16).padStart(8, '0');
}

// A placeholder id for a save still in flight. It stays deterministic per title+artist (a repeat
// save lands on the same row) but is hashed into the safe id shape, since track ids become URL
// path segments and file names and asTrackId refuses anything else.
const OPTIMISTIC_ID_PREFIX = 'optimistic-';

export function optimisticTrackId(body: CreateTrackRequest): TrackId {
  const identity = `${body.title}\u0000${body.artist}`;
  return asTrackId(`${OPTIMISTIC_ID_PREFIX}${fnv1aHex(identity, FNV_OFFSET_BASIS)}`);
}

export function isOptimisticTrackId(id: TrackId): boolean {
  return id.startsWith(OPTIMISTIC_ID_PREFIX);
}

// The key that names one logical save. The server collapses two creates carrying the same
// Idempotency-Key onto a single library row, so a row's own quick-save and the same track's
// "Save all" dispatch must present the same key or they write that row twice (#1658).
//
// Hashed rather than spelled out because the key travels in an HTTP header, which carries
// neither a 300-character title (the server caps the key at 200) nor the newlines and
// non-ASCII one may contain. Two FNV-1a passes give 64 bits, so two distinct tracks landing
// on one key — and so on one row — is not the birthday risk a single 32-bit pass would be.
//
// The album is part of the derivation because the server's own content dedup is per
// (title, artist, album): keying on less would merge two tracks it deliberately keeps apart.
// Undefined for a track with no usable identity, leaving the api-client's per-attempt key.
export function saveIdempotencyKey(body: CreateTrackRequest): string | undefined {
  const identity = trackIdentityKey(body.title, body.artist);
  if (identity === null) {
    return undefined;
  }
  const album = (body.album ?? '').trim().toLowerCase();
  // Album first and length-prefixed so no (album, title, artist) triple can slide across the
  // field boundaries onto another's key — the reason trackIdentityKey length-prefixes too.
  const canonical = `${album.length}:${album}:${identity}`;
  return `save-${fnv1aHex(canonical, FNV_OFFSET_BASIS)}${fnv1aHex(canonical, FNV_PRIME)}`;
}

export function optimisticTrack(body: CreateTrackRequest, addedAt: string): TrackResponse {
  return {
    id: optimisticTrackId(body),
    title: body.title,
    artist: body.artist,
    album: body.album,
    duration_seconds: body.duration_seconds,
    added_at: addedAt,
    acquisition_status: 'pending',
    artwork_url: body.artwork_url,
    failure_reason: null,
    year: body.year ?? null,
    genre: body.genre ?? null,
    track_number: body.track_number ?? null,
    album_artist: body.album_artist ?? null,
    isrc: body.isrc ?? null,
    audio_ref: null,
    ...(body.featured_artists ? { featured_artists: body.featured_artists } : {}),
  };
}
