declare const trackIdBrand: unique symbol;
export type TrackId = string & { readonly [trackIdBrand]: true };

declare const playlistIdBrand: unique symbol;
export type PlaylistId = string & { readonly [playlistIdBrand]: true };

declare const favoriteKeyBrand: unique symbol;
export type FavoriteKey = string & { readonly [favoriteKeyBrand]: true };

export function asTrackId(value: string): TrackId {
  return value as TrackId;
}

// An id that is safe to embed in a cache file name or a URL path segment: no `/`, `.`, `?`,
// `#` or `%`, so it can never traverse a directory or change which route a request hits.
const SAFE_ID_FORMAT = /^[A-Za-z0-9_-]{1,128}$/;

export type TrackIdResult =
  | { ok: true; id: TrackId }
  | { ok: false; error: { kind: 'invalid-track-id'; value: string } };

export function parseTrackId(value: string): TrackIdResult {
  return SAFE_ID_FORMAT.test(value)
    ? { ok: true, id: value as TrackId }
    : { ok: false, error: { kind: 'invalid-track-id', value } };
}

// Trusted ids (parsed server responses, test fixtures) go through `asPlaylistId`; untrusted
// input such as a deep-link route param must go through `parsePlaylistId`.
export function asPlaylistId(value: string): PlaylistId {
  return value as PlaylistId;
}

export type PlaylistIdResult =
  | { ok: true; id: PlaylistId }
  | { ok: false; error: { kind: 'invalid-playlist-id'; value: string } };

export function parsePlaylistId(value: string): PlaylistIdResult {
  return SAFE_ID_FORMAT.test(value)
    ? { ok: true, id: value as PlaylistId }
    : { ok: false, error: { kind: 'invalid-playlist-id', value } };
}

export function asFavoriteKey(value: string): FavoriteKey {
  return value as FavoriteKey;
}
