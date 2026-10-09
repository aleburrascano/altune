import type { QueryClient, QueryKey } from '@tanstack/react-query';

import type { TrackId } from '@shared/api-client/ids';
import type { TrackResponse } from '@shared/api-client/trackResponse';
import {
  RESIZE_POLICY,
  TRACK_CACHE_FAMILY_LIST,
  TRACK_SHAPES,
  type MapItems,
  type TrackCacheFamily,
  type TrackCacheShape,
  type TrackPages,
} from '@shared/events/trackCacheShapes';
import { flushTrackCachePatches } from '@shared/events/trackPatchBatch';

export interface TrackCachePlacement {
  readonly queryKey: QueryKey;
  readonly shape: TrackCacheShape;
  readonly pageIndex: number;
  readonly indices: readonly number[];
  readonly track: TrackResponse;
}

function indicesOf(items: readonly TrackResponse[], trackId: TrackId): number[] {
  const out: number[] = [];
  items.forEach((t, i) => {
    if (t.id === trackId) out.push(i);
  });
  return out;
}

interface TrackList {
  readonly queryKey: QueryKey;
  readonly shape: TrackCacheShape;
  readonly pageIndex: number;
  readonly items: readonly TrackResponse[];
}

function pagedLists(queryKey: QueryKey, cached: TrackPages): TrackList[] {
  return cached.pages.map((page, pageIndex) => ({
    queryKey,
    shape: 'paged',
    pageIndex,
    items: page.items,
  }));
}

function listsOf(queryKey: QueryKey, shape: TrackCacheShape, cached: unknown): TrackList[] {
  if (shape === 'paged') return pagedLists(queryKey, cached as TrackPages);
  return [{ queryKey, shape, pageIndex: 0, items: TRACK_SHAPES[shape].items(cached) }];
}

function listsInFamily(queryClient: QueryClient, family: TrackCacheFamily): TrackList[] {
  return queryClient
    .getQueriesData({ queryKey: family.prefix })
    .filter(([, cached]) => cached)
    .flatMap(([queryKey, cached]) => listsOf(queryKey, family.shape, cached));
}

function placementIn(list: TrackList, trackId: TrackId): TrackCachePlacement | undefined {
  const indices = indicesOf(list.items, trackId);
  const track = list.items[indices[0] ?? -1];
  const { queryKey, shape, pageIndex } = list;
  return track ? { queryKey, shape, pageIndex, indices, track } : undefined;
}

export function captureTrackPlacements(
  queryClient: QueryClient,
  trackId: TrackId,
): TrackCachePlacement[] {
  flushTrackCachePatches();
  return TRACK_CACHE_FAMILY_LIST.flatMap((family) => listsInFamily(queryClient, family))
    .map((list) => placementIn(list, trackId))
    .filter((placement) => placement !== undefined);
}

function reinsert(items: TrackResponse[], placement: TrackCachePlacement): TrackResponse[] {
  const next = [...items];
  for (const index of placement.indices) {
    next.splice(Math.min(index, next.length), 0, placement.track);
  }
  return next;
}

function reinsertOnPage(placement: TrackCachePlacement): MapItems {
  return (items, pageIndex) =>
    pageIndex === placement.pageIndex ? reinsert(items, placement) : items;
}

function canReinsert(placement: TrackCachePlacement, prev: unknown): boolean {
  const present = TRACK_SHAPES[placement.shape]
    .items(prev)
    .some((t) => t.id === placement.track.id);
  if (present) return false;
  return (
    placement.shape !== 'paged' || !!(prev as TrackPages | undefined)?.pages[placement.pageIndex]
  );
}

function restoreOne(queryClient: QueryClient, placement: TrackCachePlacement): void {
  const accessor = TRACK_SHAPES[placement.shape];
  queryClient.setQueryData<unknown>(placement.queryKey, (prev: unknown) =>
    canReinsert(placement, prev)
      ? accessor.write(prev, reinsertOnPage(placement), RESIZE_POLICY)
      : prev,
  );
}

export function restoreTrackPlacements(
  queryClient: QueryClient,
  placements: readonly TrackCachePlacement[],
): void {
  flushTrackCachePatches();
  for (const placement of placements) restoreOne(queryClient, placement);
}
