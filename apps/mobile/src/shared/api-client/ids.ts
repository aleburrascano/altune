declare const trackIdBrand: unique symbol;
export type TrackId = string & { readonly [trackIdBrand]: true };

declare const playlistIdBrand: unique symbol;
export type PlaylistId = string & { readonly [playlistIdBrand]: true };

declare const favoriteKeyBrand: unique symbol;
export type FavoriteKey = string & { readonly [favoriteKeyBrand]: true };

export function asTrackId(value: string): TrackId {
  return value as TrackId;
}

export function asPlaylistId(value: string): PlaylistId {
  return value as PlaylistId;
}

export function asFavoriteKey(value: string): FavoriteKey {
  return value as FavoriteKey;
}
