import { ContractError } from '@shared/errors';

declare const trackIdBrand: unique symbol;
export type TrackId = string & { readonly [trackIdBrand]: true };

declare const playlistIdBrand: unique symbol;
export type PlaylistId = string & { readonly [playlistIdBrand]: true };

declare const favoriteKeyBrand: unique symbol;
export type FavoriteKey = string & { readonly [favoriteKeyBrand]: true };

// An id that is safe to embed in a cache file name or a URL path segment: no `/`, `.`, `?`,
// `#` or `%`, so it can never traverse a directory or change which route a request hits.
// Server ids (UUIDs) and client placeholder ids all fit this opaque-token shape.
const SAFE_ID_FORMAT = /^[A-Za-z0-9_-]{1,128}$/;

// The format admits these, but they are the names an object already inherits. `record[id] = entry`
// on a plain object keyed by `__proto__` runs Object.prototype's accessor instead of defining an
// own property: the entry vanishes from Object.keys and JSON.stringify, and the record's prototype
// is replaced for the rest of its life. Refusing them as ids keeps every id-keyed record honest
// without each keying site owning a guard.
const RESERVED_OBJECT_KEYS: ReadonlySet<string> = new Set(['__proto__', 'constructor', 'prototype']);

export function isSafeId(value: string): boolean {
  return SAFE_ID_FORMAT.test(value) && !RESERVED_OBJECT_KEYS.has(value);
}

export type TrackIdResult =
  | { ok: true; id: TrackId }
  | { ok: false; error: { kind: 'invalid-track-id'; value: string } };

export function parseTrackId(value: string): TrackIdResult {
  return isSafeId(value)
    ? { ok: true, id: value as TrackId }
    : { ok: false, error: { kind: 'invalid-track-id', value } };
}

// Trusted ids (parsed server responses, test fixtures) go through the throwing `asTrackId` /
// `asPlaylistId`; untrusted input such as a deep-link route param goes through the non-throwing
// `parseTrackId` / `parsePlaylistId`. Either way a branded id always has the safe shape.
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

// The one deliberate exception to the shape: "no playlist" for hooks that must be called
// unconditionally (a screen whose route param failed to parse). idPathSegment refuses it, so it
// can never reach a request path.
export const NO_PLAYLIST_ID = '' as PlaylistId;

// Every track/playlist id reaches a URL path through this one function, so no call site can
// forget to escape it. The shape is re-checked because escaping alone cannot neutralise a `..`
// id (URL resolution collapses it, even as `%2e%2e`) and a cast can still smuggle a raw string in.
export function idPathSegment(id: TrackId | PlaylistId): string {
  if (!isSafeId(id)) throw new ContractError('IdPathSegment', 'not a safe URL path segment');
  return encodeURIComponent(id);
}

export function asFavoriteKey(value: string): FavoriteKey {
  return value as FavoriteKey;
}
