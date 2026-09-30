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

function fnv1aHex(input: string, basis: number): string {
  let hash = basis;
  for (const ch of input) {
    hash ^= ch.codePointAt(0) ?? 0;
    hash = Math.imul(hash, FNV_PRIME) >>> 0;
  }
  return hash.toString(16).padStart(8, '0');
}

function normalizedAlbum(body: CreateTrackRequest): string {
  return (body.album ?? '').trim().toLowerCase();
}

const OPTIMISTIC_ID_PREFIX = 'optimistic-';

export function optimisticTrackId(body: CreateTrackRequest): TrackId {
  const identity = `${body.title}\u0000${body.artist}\u0000${normalizedAlbum(body)}`;
  return asTrackId(`${OPTIMISTIC_ID_PREFIX}${fnv1aHex(identity, FNV_OFFSET_BASIS)}`);
}

export function isOptimisticTrackId(id: TrackId): boolean {
  return id.startsWith(OPTIMISTIC_ID_PREFIX);
}

export function saveIdempotencyKey(body: CreateTrackRequest): string | undefined {
  const identity = trackIdentityKey(body.title, body.artist);
  if (identity === null) {
    return undefined;
  }
  const album = normalizedAlbum(body);
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
