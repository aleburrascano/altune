import { ContractError, fail, ok, type Result } from '@shared/failure';

declare const trackIdBrand: unique symbol;
export type TrackId = string & { readonly [trackIdBrand]: true };

declare const playlistIdBrand: unique symbol;
export type PlaylistId = string & { readonly [playlistIdBrand]: true };

declare const favoriteKeyBrand: unique symbol;
export type FavoriteKey = string & { readonly [favoriteKeyBrand]: true };

const SAFE_ID_FORMAT = /^[A-Za-z0-9_-]{1,128}$/;

const RESERVED_OBJECT_KEYS: ReadonlySet<string> = new Set([
  '__proto__',
  'constructor',
  'prototype',
]);

export function isSafeId(value: string): boolean {
  return SAFE_ID_FORMAT.test(value) && !RESERVED_OBJECT_KEYS.has(value);
}

export interface InvalidTrackId {
  readonly kind: 'invalid-track-id';
  readonly value: string;
}

export type TrackIdResult = Result<TrackId, InvalidTrackId>;

export function parseTrackId(value: string): TrackIdResult {
  return isSafeId(value) ? ok(value as TrackId) : fail({ kind: 'invalid-track-id', value });
}

export function asTrackId(value: string): TrackId {
  const parsed = parseTrackId(value);
  if (!parsed.ok) throw new ContractError('TrackId', 'not a valid id shape');
  return parsed.value;
}

export interface InvalidPlaylistId {
  readonly kind: 'invalid-playlist-id';
  readonly value: string;
}

export type PlaylistIdResult = Result<PlaylistId, InvalidPlaylistId>;

export function parsePlaylistId(value: string): PlaylistIdResult {
  return isSafeId(value) ? ok(value as PlaylistId) : fail({ kind: 'invalid-playlist-id', value });
}

export function asPlaylistId(value: string): PlaylistId {
  const parsed = parsePlaylistId(value);
  if (!parsed.ok) throw new ContractError('PlaylistId', 'not a valid id shape');
  return parsed.value;
}

export const NO_PLAYLIST_ID = '' as PlaylistId;

export function idPathSegment(id: TrackId | PlaylistId): string {
  if (!isSafeId(id)) throw new ContractError('IdPathSegment', 'not a safe URL path segment');
  return encodeURIComponent(id);
}

export function asFavoriteKey(value: string): FavoriteKey {
  return value as FavoriteKey;
}

export type ProviderIdPair = { provider: string; external_id: string };

export function sourceIdList(sources: readonly ProviderIdPair[]): string {
  return sources.map((s) => `${s.provider}:${s.external_id}`).join(',');
}
