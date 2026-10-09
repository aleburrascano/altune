import type { QueryClient, QueryKey } from '@tanstack/react-query';

import type { TrackId } from '@shared/api-client/ids';
import type { AcquisitionTransition } from '@shared/api-client/trackAcquisition';
import type { TrackFields, TrackResponse } from '@shared/api-client/trackResponse';
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

export function restoreTrackPlacements(
  queryClient: QueryClient,
  placements: readonly TrackCachePlacement[],
): void {
  flushTrackCachePatches();
  for (const placement of placements) {
    const id = placement.track.id;
    const accessor = TRACK_SHAPES[placement.shape];
    const reinsertHere: MapItems = (items, pageIndex) =>
      pageIndex === placement.pageIndex ? reinsert(items, placement) : items;
    queryClient.setQueryData<unknown>(placement.queryKey, (prev: unknown) => {
      if (accessor.items(prev).some((t) => t.id === id)) return prev;
      if (
        placement.shape === 'paged' &&
        !(prev as TrackPages | undefined)?.pages[placement.pageIndex]
      ) {
        return prev;
      }
      return accessor.write(prev, reinsertHere, RESIZE_POLICY);
    });
  }
}

export type TrackPatch = Partial<TrackFields> &
  (
    | AcquisitionTransition
    | { acquisition_status?: never; failure_reason?: never; failure_message?: never }
  );

function withPatch(track: TrackResponse, patch: TrackPatch | undefined): TrackResponse {
  return patch ? { ...track, ...patch } : track;
}

interface PendingTrackPatches {
  readonly queryClient: QueryClient;
  readonly byTrackId: Map<TrackId, TrackPatch>;
}

let pendingPatches: PendingTrackPatches | null = null;

export function scheduleTrackPatch(
  queryClient: QueryClient,
  trackId: TrackId,
  patch: TrackPatch,
): void {
  if (pendingPatches && pendingPatches.queryClient !== queryClient) flushTrackCachePatches();
  if (pendingPatches === null) {
    pendingPatches = { queryClient, byTrackId: new Map() };
    void Promise.resolve().then(flushTrackCachePatches);
  }
  const prior = pendingPatches.byTrackId.get(trackId);
  pendingPatches.byTrackId.set(trackId, prior ? { ...prior, ...patch } : patch);
}

function pendingPatchFor(queryClient: QueryClient, trackId: TrackId): TrackPatch | undefined {
  return pendingPatches?.queryClient === queryClient
    ? pendingPatches.byTrackId.get(trackId)
    : undefined;
}

function flushTrackCachePatches(): void {
  const batch = pendingPatches;
  if (batch === null) return;
  pendingPatches = null;
  const applyBatch: MapItems = (items) => items.map((t) => withPatch(t, batch.byTrackId.get(t.id)));
  for (const family of TRACK_CACHE_FAMILY_LIST) {
    writeFamily(batch.queryClient, family, { queryKey: family.prefix }, applyBatch, PATCH_POLICY);
    replayOverInFlightFetches(batch.queryClient, family, applyBatch);
  }
}

export function patchTrackInCaches(
  queryClient: QueryClient,
  trackId: TrackId,
  patch: TrackPatch,
): void {
  scheduleTrackPatch(queryClient, trackId, patch);
  flushTrackCachePatches();
}

function replayOverInFlightFetches(
  queryClient: QueryClient,
  family: TrackCacheFamily,
  mapItems: MapItems,
): void {
  const cache = queryClient.getQueryCache();
  const inFlight = cache
    .findAll({ queryKey: family.prefix })
    .filter((query) => query.state.fetchStatus !== 'idle');
  for (const query of inFlight) {
    const unsubscribe = cache.subscribe((event) => {
      if (event.query !== query) return;
      if (event.type === 'removed') return unsubscribe();
      if (event.type !== 'updated') return;
      if (event.action.type === 'success' && !event.action.manual) {
        const exactQuery = { queryKey: query.queryKey, exact: true };
        writeFamily(queryClient, family, exactQuery, mapItems, PATCH_POLICY);
      }
      if (query.state.fetchStatus === 'idle') unsubscribe();
    });
  }
}
