import { asFavoriteKey } from '@shared/api-client/ids';
import type { Favorite, FavoriteRef } from '@shared/api-client/favorites';

import type { FavoritesPort } from '@shared/favorites/ports/favoritesPort';

function sameEntry(a: Favorite, b: Favorite): boolean {
  return a.kind === b.kind && a.key === b.key;
}

function favoriteOf(ref: FavoriteRef): Favorite {
  return {
    kind: ref.kind,
    key: asFavoriteKey(ref.title),
    title: ref.title,
    subtitle: ref.subtitle,
    ...(ref.image_url !== undefined ? { image_url: ref.image_url } : {}),
  };
}

export function createMemoryFavoritesPort(seed: Favorite[] = []): FavoritesPort {
  let stored = [...seed];
  return {
    listFavorites: async () => ({ items: [...stored], total: stored.length }),
    addFavorite: async (ref) => {
      const added = favoriteOf(ref);
      if (!stored.some((f) => sameEntry(f, added))) stored = [added, ...stored];
      return added;
    },
    removeFavorite: async (ref) => {
      const removed = favoriteOf(ref);
      stored = stored.filter((f) => !sameEntry(f, removed));
    },
  };
}
