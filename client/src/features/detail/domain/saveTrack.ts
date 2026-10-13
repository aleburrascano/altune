import { trackIdentityKey } from '@shared/acquisition/trackStatusStore';
import { asTrackId, type TrackId } from '@shared/apiClient/ids';
import type { CreateTrackRequest, TrackResponse } from '@shared/apiClient/trackResponse';
import type { DiscoveryResult } from '@shared/apiClient/discovery';

import { trackExtras } from '@features/detail/domain/trackExtras';
import type { OwnedTrack } from '@features/detail/domain/ownedTrack';

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

export type SaveFailure = {
  message: string;
  isRetryable: boolean;
  trackId?: TrackId;
};

export type SaveControlState = 'add' | 'saving' | 'ready' | 'failed' | 'rejected';

export type SaveState = SaveControlState | 'disabled';

export function saveControlInteractive(state: SaveState): boolean {
  return state === 'add' || state === 'failed';
}

export function saveDisplayState(state: SaveState): SaveControlState {
  return state === 'disabled' ? 'add' : state;
}

export function saveControlState(owned: OwnedTrack | null): SaveControlState {
  if (owned === null) {
    return 'add';
  }
  if (owned.acquisitionStatus === 'failed') {
    return 'failed';
  }
  if (owned.acquisitionStatus === 'pending') {
    return 'saving';
  }
  return 'ready';
}

export function saveControlLabel(state: SaveControlState, title: string): string {
  switch (state) {
    case 'saving':
      return `${title} downloading`;
    case 'ready':
      return `${title} in library`;
    case 'failed':
      return `Retry saving ${title}`;
    case 'rejected':
      return `Couldn't save ${title}`;
    default:
      return `Save ${title}`;
  }
}

export function saveControlText(state: SaveControlState): string {
  switch (state) {
    case 'saving':
      return 'Saving…';
    case 'ready':
      return 'Saved';
    case 'failed':
      return 'Retry';
    case 'rejected':
      return "Can't save";
    default:
      return 'Save';
  }
}

export function ownedRetryTrackId(owned: OwnedTrack | null): TrackId | null {
  if (owned === null || owned.acquisitionStatus !== 'failed') {
    return null;
  }
  return isOptimisticTrackId(owned.trackId) ? null : owned.trackId;
}

export function saveFailureBanner(failure: SaveFailure): string {
  const nextStep = failure.isRetryable ? 'Tap Retry.' : "Retrying won't help.";
  return `Couldn't save this track. ${nextStep} (${failure.message})`;
}

export const SAVE_ALL_CONCURRENCY = 4;
