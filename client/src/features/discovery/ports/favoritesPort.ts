import type * as apiClient from '@shared/apiClient';

export interface FavoritesPort {
  listFavorites: typeof apiClient.listFavorites;
  addFavorite: typeof apiClient.addFavorite;
  removeFavorite: typeof apiClient.removeFavorite;
}
