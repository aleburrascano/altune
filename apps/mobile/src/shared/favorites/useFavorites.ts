import { useQuery } from '@tanstack/react-query';

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

function entryKey(kind: string, key: string): string {
  return `${kind}|${key}`;
}

export function useFavorites(): FavoritesApi {
  const { data } = useQuery({
    queryKey: discoveryKeys.favorites,
    queryFn: listFavorites,
    staleTime: Infinity,
  });

  const saved = new Set((data?.items ?? []).map((f) => entryKey(f.kind, f.key)));
  const isFavorite = (target: FavoriteTarget): boolean =>
    saved.has(entryKey(target.kind, target.favorite_key));

  const mutation = useOptimisticMutation({
    queryKey: discoveryKeys.favorites,
    // Deliberately unguarded: preserves this hook's pre-extraction behavior (no cancel, no
    // rollback guard). Tightening it is a behavior change for a hardening ticket.
    unguarded: true,
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
    applyOptimistic: (previous: FavoritesResponse | undefined, target) =>
      patched(previous, target, isFavorite(target)),
    // Direction-neutral: by the time this runs the optimistic write is already rolled back, so
    // the hook can no longer tell a failed save from a failed removal.
    alertOnError: () => ({
      title: 'Update failed',
      message: `Could not update your favorites. ${RETRY_TAIL}`,
    }),
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
