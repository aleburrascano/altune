import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import {
  addFavorite,
  listFavorites,
  removeFavorite,
  type FavoritesResponse,
  type FavoriteTarget,
} from '@shared/api-client/favorites';
import { discoveryKeys } from '@shared/lib/query-keys';

type FavoritesApi = {
  isFavorite: (target: FavoriteTarget) => boolean;
  toggle: (target: FavoriteTarget) => void;
};

function entryKey(kind: string, key: string): string {
  return `${kind}|${key}`;
}

export function useFavorites(): FavoritesApi {
  const queryClient = useQueryClient();

  const { data } = useQuery({
    queryKey: discoveryKeys.favorites,
    queryFn: listFavorites,
    staleTime: Infinity,
  });

  const saved = new Set((data?.items ?? []).map((f) => entryKey(f.kind, f.key)));
  const isFavorite = (target: FavoriteTarget): boolean =>
    saved.has(entryKey(target.kind, target.favorite_key));

  const mutation = useMutation({
    mutationFn: async (target: FavoriteTarget) => {
      const ref = {
        kind: target.kind,
        title: target.title,
        subtitle: target.subtitle,
        image_url: target.image_url,
      };
      if (isFavorite(target)) {
        await removeFavorite(ref);
        return;
      }
      await addFavorite(ref);
    },
    onMutate: (target: FavoriteTarget) => {
      const previous = queryClient.getQueryData<FavoritesResponse>(discoveryKeys.favorites);
      queryClient.setQueryData(
        discoveryKeys.favorites,
        patched(previous, target, isFavorite(target)),
      );
      return { previous };
    },
    onError: (_err, _target, context) => {
      queryClient.setQueryData(discoveryKeys.favorites, context?.previous);
    },
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: discoveryKeys.favorites });
    },
  });

  return { isFavorite, toggle: (target) => mutation.mutate(target) };
}

function patched(
  current: FavoritesResponse | undefined,
  target: FavoriteTarget,
  wasFavorite: boolean,
): FavoritesResponse {
  const items = current?.items ?? [];
  const next = wasFavorite
    ? items.filter((f) => entryKey(f.kind, f.key) !== entryKey(target.kind, target.favorite_key))
    : [
        {
          kind: target.kind,
          key: target.favorite_key,
          title: target.title,
          subtitle: target.subtitle,
          image_url: target.image_url,
        },
        ...items,
      ];
  return { items: next, total: next.length };
}
