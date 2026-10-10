import type * as favorites from '@shared/api-client/favorites';

export interface FavoritesPort {
  listFavorites: typeof favorites.listFavorites;
  addFavorite: typeof favorites.addFavorite;
  removeFavorite: typeof favorites.removeFavorite;
}
