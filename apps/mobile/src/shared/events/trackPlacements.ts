import type { QueryClient, QueryKey } from '@tanstack/react-query';

import type { TrackId } from '@shared/api-client/ids';
import type { TrackResponse } from '@shared/api-client/trackResponse';
import {
  RESIZE_POLICY,
  TRACK_CACHE_FAMILY_LIST,
  TRACK_SHAPES,
  type MapItems,
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

export function captureTrackPlacements(
  queryClient: QueryClient,
  trackId: TrackId,
): TrackCachePlacement[] {
  flushTrackCachePatches();
  const placements: TrackCachePlacement[] = [];
  const record = (
    queryKey: QueryKey,
    shape: TrackCacheShape,
    pageIndex: number,
    items: readonly TrackResponse[],
  ) => {
    const indices = indicesOf(items, trackId);
    const track = items[indices[0] ?? -1];
    if (track) placements.push({ queryKey, shape, pageIndex, indices, track });
  };
  for (const family of TRACK_CACHE_FAMILY_LIST) {
    for (const [queryKey, data] of queryClient.getQueriesData({ queryKey: family.prefix })) {
      if (!data) continue;
      if (family.shape === 'paged') {
        (data as TrackPages).pages.forEach((page, i) => record(queryKey, 'paged', i, page.items));
      } else {
        record(queryKey, family.shape, 0, TRACK_SHAPES[family.shape].items(data));
      }
    }
  }
  return placements;
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

export function restoreTrackPlacements(
  queryClient: QueryClient,
  placements: readonly TrackCachePlacement[],
): void {
  flushTrackCachePatches();
  for (const placement of placements) {
    const id = placement.track.id;
    const accessor = TRACK_SHAPES[placement.shape];
    queryClient.setQueryData<unknown>(placement.queryKey, (prev: unknown) => {
      if (accessor.items(prev).some((t) => t.id === id)) return prev;
      if (
        placement.shape === 'paged' &&
        !(prev as TrackPages | undefined)?.pages[placement.pageIndex]
      ) {
        return prev;
      }
      return accessor.write(prev, reinsertOnPage(placement), RESIZE_POLICY);
    });
  }
}
