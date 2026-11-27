import type { TrackId, TrackResponse } from '@shared/catalog';
import { removeTrackStatus } from '@shared/acquisition';

import type { CacheKey, EventCache } from '@shared/events/core/eventCache';
import {
  pagedAccessor,
  RESIZE_POLICY,
  TRACK_CACHE_FAMILIES,
  TRACK_CACHE_FAMILY_LIST,
  TRACK_SHAPES,
  type MapItems,
  type TrackCacheValue,
  type TrackCacheFamily,
  type TrackCacheShape,
  type TrackPages,
} from '@shared/events/core/trackCacheShapes';
import { upsertIntoPages } from '@shared/events/core/trackUpsert';
import {
  flushTrackCachePatches,
  pendingPatchFor,
  withPatch,
} from '@shared/events/shell/trackPatchBatch';
import { writeFamily } from '@shared/events/shell/writeFamily';

export function getTrackFromCaches(cache: EventCache, trackId: TrackId): TrackResponse | undefined {
  const cached = TRACK_CACHE_FAMILY_LIST.flatMap((family) =>
    cache
      .getQueriesData<TrackCacheValue>({ queryKey: family.prefix })
      .flatMap(([, data]) => TRACK_SHAPES[family.shape].items(data)),
  );
  const found = cached.find((t) => t.id === trackId);
  return found ? withPatch(found, pendingPatchFor(cache, trackId)) : undefined;
}

export function upsertTrackInCaches(cache: EventCache, track: TrackResponse): void {
  flushTrackCachePatches();
  cache.setQueriesData<TrackPages>({ queryKey: TRACK_CACHE_FAMILIES.pagedLibrary.prefix }, (prev) =>
    upsertIntoPages(prev, track),
  );
}

export function replaceTrackInCaches(
  cache: EventCache,
  optimisticId: TrackId,
  real: TrackResponse,
): void {
  flushTrackCachePatches();
  cache.setQueriesData<TrackPages>({ queryKey: TRACK_CACHE_FAMILIES.pagedLibrary.prefix }, (prev) =>
    pagedAccessor.write(
      prev,
      (items) => dedupById(items.map((t) => (t.id === optimisticId ? real : t))),
      RESIZE_POLICY,
    ),
  );
}

function dedupById(items: TrackResponse[]): TrackResponse[] {
  const seen = new Set<string>();
  return items.filter((t) => (seen.has(t.id) ? false : (seen.add(t.id), true)));
}

export function removeTrackFromCaches(cache: EventCache, trackId: TrackId): void {
  flushTrackCachePatches();
  const drop: MapItems = (items) => items.filter((t) => t.id !== trackId);
  for (const family of TRACK_CACHE_FAMILY_LIST) {
    writeFamily(cache, family, { queryKey: family.prefix }, drop, RESIZE_POLICY);
  }
}

export function forgetTrack(cache: EventCache, trackId: TrackId): void {
  removeTrackFromCaches(cache, trackId);
  removeTrackStatus(trackId);
}

export interface TrackCachePlacement {
  readonly queryKey: CacheKey;
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
  readonly queryKey: CacheKey;
  readonly shape: TrackCacheShape;
  readonly pageIndex: number;
  readonly items: readonly TrackResponse[];
}

function pagedLists(queryKey: CacheKey, cached: TrackPages): TrackList[] {
  return cached.pages.map((page, pageIndex) => ({
    queryKey,
    shape: 'paged',
    pageIndex,
    items: page.items,
  }));
}

function listsOf(queryKey: CacheKey, shape: TrackCacheShape, cached: TrackCacheValue): TrackList[] {
  if (shape === 'paged') return pagedLists(queryKey, cached as TrackPages);
  return [{ queryKey, shape, pageIndex: 0, items: TRACK_SHAPES[shape].items(cached) }];
}

function listsInFamily(cache: EventCache, family: TrackCacheFamily): TrackList[] {
  const lists: TrackList[] = [];
  for (const [queryKey, cached] of cache.getQueriesData<TrackCacheValue>({
    queryKey: family.prefix,
  })) {
    if (cached) lists.push(...listsOf(queryKey, family.shape, cached));
  }
  return lists;
}

function placementIn(list: TrackList, trackId: TrackId): TrackCachePlacement | undefined {
  const indices = indicesOf(list.items, trackId);
  const track = list.items[indices[0] ?? -1];
  const { queryKey, shape, pageIndex } = list;
  return track ? { queryKey, shape, pageIndex, indices, track } : undefined;
}

function placementsInFamily(
  cache: EventCache,
  family: TrackCacheFamily,
  trackId: TrackId,
): TrackCachePlacement[] {
  return listsInFamily(cache, family)
    .map((list) => placementIn(list, trackId))
    .filter((placement): placement is TrackCachePlacement => placement !== undefined);
}

export function captureTrackPlacements(cache: EventCache, trackId: TrackId): TrackCachePlacement[] {
  flushTrackCachePatches();
  return TRACK_CACHE_FAMILY_LIST.flatMap((family) => placementsInFamily(cache, family, trackId));
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

function holdsTrack(placement: TrackCachePlacement, prev: TrackCacheValue | undefined): boolean {
  return TRACK_SHAPES[placement.shape].items(prev).some((t) => t.id === placement.track.id);
}

function restoreOne(cache: EventCache, placement: TrackCachePlacement): void {
  const accessor = TRACK_SHAPES[placement.shape];
  cache.setQueryData<TrackCacheValue>(placement.queryKey, (prev) =>
    holdsTrack(placement, prev)
      ? prev
      : accessor.write(prev, reinsertOnPage(placement), RESIZE_POLICY),
  );
}

export function restoreTrackPlacements(
  cache: EventCache,
  placements: readonly TrackCachePlacement[],
): void {
  flushTrackCachePatches();
  for (const placement of placements) restoreOne(cache, placement);
}
