import { ContractError } from '@shared/errors';

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

export type TrackIdResult =
  { ok: true; id: TrackId } | { ok: false; error: { kind: 'invalid-track-id'; value: string } };

export function parseTrackId(value: string): TrackIdResult {
  return isSafeId(value)
    ? { ok: true, id: value as TrackId }
    : { ok: false, error: { kind: 'invalid-track-id', value } };
}

export function asTrackId(value: string): TrackId {
  const parsed = parseTrackId(value);
  if (!parsed.ok) throw new ContractError('TrackId', 'not a valid id shape');
  return parsed.id;
}

export type PlaylistIdResult =
  | { ok: true; id: PlaylistId }
  | { ok: false; error: { kind: 'invalid-playlist-id'; value: string } };

export function parsePlaylistId(value: string): PlaylistIdResult {
  return isSafeId(value)
    ? { ok: true, id: value as PlaylistId }
    : { ok: false, error: { kind: 'invalid-playlist-id', value } };
}

export function asPlaylistId(value: string): PlaylistId {
  const parsed = parsePlaylistId(value);
  if (!parsed.ok) throw new ContractError('PlaylistId', 'not a valid id shape');
  return parsed.id;
}

export const NO_PLAYLIST_ID = '' as PlaylistId;

export function idPathSegment(id: TrackId | PlaylistId): string {
  if (!isSafeId(id)) throw new ContractError('IdPathSegment', 'not a safe URL path segment');
  return encodeURIComponent(id);
}

export function discoveryEntityPath(
  kind: 'albums' | 'artists' | 'tracks',
  provider: string,
  externalId: string,
  tail: string,
): string {
  return `/v1/discovery/${kind}/${encodeURIComponent(provider)}/${encodeURIComponent(externalId)}/${tail}`;
}

export function asFavoriteKey(value: string): FavoriteKey {
  return value as FavoriteKey;
}
