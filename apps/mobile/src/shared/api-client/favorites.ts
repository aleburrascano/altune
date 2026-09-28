import { DISCOVERY_KINDS } from './discovery';
import { apiFetch, apiSend } from './index';
import { asFavoriteKey } from './ids';
import { asRecord, asString, member, parseListEnvelope } from './wireDecoders';
import type { DiscoveryKind } from './discovery';
import type { FavoriteKey } from './ids';

export type Favorite = {
  kind: DiscoveryKind;
  key: FavoriteKey;
  title: string;
  subtitle?: string | undefined;
  image_url?: string | undefined;
};

export type FavoritesResponse = {
  items: Favorite[];
  total: number;
};

export type FavoriteRef = {
  kind: DiscoveryKind;
  title: string;
  subtitle: string;
  image_url?: string | undefined;
};

export type FavoriteTarget = FavoriteRef & { favorite_key: FavoriteKey };

function parseFavorite(value: unknown, at = 'Favorite'): Favorite {
  const r = asRecord(value, at);
  return {
    kind: member(r.kind, DISCOVERY_KINDS, `${at}.kind`),
    key: asFavoriteKey(asString(r.key, `${at}.key`)),
    title: asString(r.title, `${at}.title`),
    ...(r.subtitle != null ? { subtitle: asString(r.subtitle, `${at}.subtitle`) } : {}),
    ...(r.image_url != null ? { image_url: asString(r.image_url, `${at}.image_url`) } : {}),
  };
}

function parseFavoritesResponse(value: unknown, at = 'FavoritesResponse'): FavoritesResponse {
  const r = asRecord(value, at);
  return parseListEnvelope(r, at, parseFavorite);
}

export async function listFavorites(): Promise<FavoritesResponse> {
  return parseFavoritesResponse(await apiFetch<unknown>('/v1/discovery/favorites'));
}

export async function addFavorite(ref: FavoriteRef): Promise<Favorite> {
  return parseFavorite(await apiSend<unknown>('/v1/discovery/favorites', 'PUT', ref));
}

export async function removeFavorite(ref: FavoriteRef): Promise<void> {
  await apiSend<void>('/v1/discovery/favorites', 'DELETE', ref);
}
