declare const trackIdBrand: unique symbol;
export type TrackId = string & { readonly [trackIdBrand]: true };

declare const playlistIdBrand: unique symbol;
export type PlaylistId = string & { readonly [playlistIdBrand]: true };

declare const favoriteKeyBrand: unique symbol;
export type FavoriteKey = string & { readonly [favoriteKeyBrand]: true };

export function asTrackId(value: string): TrackId {
  return value as TrackId;
}

// A track id that is safe to embed in a cache file name or a URL path segment: no `/`, `.`, `?`,
// `#` or `%`, so it can never traverse a directory or change which route a request hits.
const TRACK_ID_FORMAT = /^[A-Za-z0-9_-]{1,128}$/;

export type TrackIdResult =
  | { ok: true; id: TrackId }
  | { ok: false; error: { kind: 'invalid-track-id'; value: string } };

export function parseTrackId(value: string): TrackIdResult {
  return TRACK_ID_FORMAT.test(value)
    ? { ok: true, id: value as TrackId }
    : { ok: false, error: { kind: 'invalid-track-id', value } };
}

export function asPlaylistId(value: string): PlaylistId {
  return value as PlaylistId;
}

export function asFavoriteKey(value: string): FavoriteKey {
  return value as FavoriteKey;
}
