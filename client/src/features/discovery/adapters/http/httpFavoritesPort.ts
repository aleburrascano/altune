import { addFavorite, listFavorites, removeFavorite } from '@shared/apiClient/favorites';

import type { FavoritesPort } from '@features/discovery/ports/favoritesPort';

export const httpFavoritesPort: FavoritesPort = {
  listFavorites: (...args) => listFavorites(...args),
  addFavorite: (...args) => addFavorite(...args),
  removeFavorite: (...args) => removeFavorite(...args),
};
