import { useQuery, useQueryClient } from '@tanstack/react-query';

import {
  addFavorite,
  listFavorites,
  removeFavorite,
  type FavoritesResponse,
  type FavoriteTarget,
} from '@shared/api-client/favorites';
import { RETRY_TAIL } from '@shared/lib/describeError';
import { discoveryKeys } from '@shared/lib/query-keys';
import { useOptimisticMutation } from '@shared/query/useOptimisticMutation';

type FavoritesApi = {
  isFavorite: (target: FavoriteTarget) => boolean;
  toggle: (target: FavoriteTarget) => void;
};

type ToggleRequest = { target: FavoriteTarget; wasFavorite: boolean };

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

  const mutation = useOptimisticMutation({
    action: 'favorites.toggle',
    queryKey: discoveryKeys.favorites,
    unguarded: true,
    mutationFn: sendToggle,
    applyOptimistic: (previous: FavoritesResponse | undefined, { target, wasFavorite }) =>
      patched(previous, target, wasFavorite),
    alertOnError: () => ({
      title: 'Update failed',
      message: `Could not update your favorites. ${RETRY_TAIL}`,
    }),
  });

  const toggle = (target: FavoriteTarget): void => {
    const current = queryClient.getQueryData<FavoritesResponse>(discoveryKeys.favorites);
    const wasFavorite = (current?.items ?? []).some(
      (f) => entryKey(f.kind, f.key) === entryKey(target.kind, target.favorite_key),
    );
    mutation.mutate({ target, wasFavorite });
  };

  return { isFavorite, toggle };
}

async function sendToggle({ target, wasFavorite }: ToggleRequest): Promise<void> {
  const { kind, title, subtitle, image_url } = target;
  const send = wasFavorite ? removeFavorite : addFavorite;
  await send({ kind, title, subtitle, image_url });
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
