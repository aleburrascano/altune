import { apiFetch } from './index';
import type { DiscoveryKind } from './discovery';

export type Favorite = {
  kind: DiscoveryKind;
  key: string;
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

export type FavoriteTarget = FavoriteRef & { favorite_key: string };

export async function listFavorites(): Promise<FavoritesResponse> {
  return apiFetch<FavoritesResponse>('/v1/discovery/favorites');
}

export async function addFavorite(ref: FavoriteRef): Promise<Favorite> {
  return apiFetch<Favorite>('/v1/discovery/favorites', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(ref),
  });
}

export async function removeFavorite(ref: FavoriteRef): Promise<void> {
  await apiFetch<void>('/v1/discovery/favorites', {
    method: 'DELETE',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(ref),
  });
}
