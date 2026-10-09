import { addFavorite, listFavorites, removeFavorite } from '@shared/api-client/favorites';

import type { FavoritesPort } from '@shared/favorites/ports/favoritesPort';

export const httpFavoritesPort: FavoritesPort = {
  listFavorites: (...args) => listFavorites(...args),
  addFavorite: (...args) => addFavorite(...args),
  removeFavorite: (...args) => removeFavorite(...args),
};
