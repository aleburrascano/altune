import type { QueryClient, QueryKey } from '@tanstack/react-query';

import type { TrackId } from '@shared/api-client/ids';
import type { TrackResponse } from '@shared/api-client/trackResponse';
import { removeTrackStatus } from '@shared/acquisition/trackStatusStore';
import { libraryKeys } from '@shared/query/queryKeys';
import {
  pagedAccessor,
  PATCH_POLICY,
  reflowOffsets,
  RESIZE_POLICY,
  TRACK_CACHE_FAMILIES,
  TRACK_CACHE_FAMILY_LIST,
  TRACK_SHAPES,
  writeFamily,
  type MapItems,
  type TrackCacheFamily,
  type TrackCacheShape,
  type TrackPages,
} from '@shared/events/trackCacheShapes';
import { flushTrackCachePatches, pendingPatchFor, withPatch } from '@shared/events/trackPatchBatch';

const LIBRARY_DERIVED_KEYS: readonly (readonly string[])[] = [
  libraryKeys.albumsPrefix,
  libraryKeys.artistsPrefix,
  libraryKeys.summary,
  libraryKeys.lookupPrefix,
];

export function invalidateLibraryDerived(queryClient: QueryClient): void {
  for (const queryKey of LIBRARY_DERIVED_KEYS) {
    void queryClient.invalidateQueries({ queryKey });
  }
}

export function getTrackFromCaches(
  queryClient: QueryClient,
  trackId: TrackId,
): TrackResponse | undefined {
  for (const family of TRACK_CACHE_FAMILY_LIST) {
    const entries = queryClient.getQueriesData({ queryKey: family.prefix });
    for (const [, data] of entries) {
      const found = TRACK_SHAPES[family.shape].items(data).find((t) => t.id === trackId);
      if (found) return withPatch(found, pendingPatchFor(queryClient, trackId));
    }
  }
  return undefined;
}

function mergeTrack(prev: TrackResponse, next: TrackResponse): TrackResponse {
  const merged = { ...prev, ...next };
  if (next.failure_message === undefined) delete merged.failure_message;
  return merged as TrackResponse;
}

export function upsertTrackInCaches(queryClient: QueryClient, track: TrackResponse): void {
  flushTrackCachePatches();
  queryClient.setQueriesData<TrackPages>(
    { queryKey: TRACK_CACHE_FAMILIES.pagedLibrary.prefix },
    (prev) => {
      if (!prev) return prev;
      const known = prev.pages.some((page) => page.items.some((t) => t.id === track.id));
      if (known) {
        return pagedAccessor.write(
          prev,
          (items) => items.map((t) => (t.id === track.id ? mergeTrack(t, track) : t)),
          PATCH_POLICY,
        );
      }
      const [first, ...rest] = prev.pages;
      if (!first) return prev;
      const pages = [{ ...first, items: [track, ...first.items], total: first.total + 1 }, ...rest];
      return { ...prev, pages: reflowOffsets(prev.pages, pages) };
    },
  );
}

export function replaceTrackInCaches(
  queryClient: QueryClient,
  optimisticId: TrackId,
  real: TrackResponse,
): void {
  flushTrackCachePatches();
  queryClient.setQueriesData<TrackPages>(
    { queryKey: TRACK_CACHE_FAMILIES.pagedLibrary.prefix },
    (prev) =>
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

export function removeTrackFromCaches(queryClient: QueryClient, trackId: TrackId): void {
  flushTrackCachePatches();
  const drop: MapItems = (items) => items.filter((t) => t.id !== trackId);
  for (const family of TRACK_CACHE_FAMILY_LIST) {
    writeFamily(queryClient, family, { queryKey: family.prefix }, drop, RESIZE_POLICY);
  }
}

export function forgetTrack(queryClient: QueryClient, trackId: TrackId): void {
  removeTrackFromCaches(queryClient, trackId);
  removeTrackStatus(trackId);
}

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
