import type { QueryClient } from '@tanstack/react-query';

import type { TrackId } from '@shared/api-client/ids';
import type { TrackResponse } from '@shared/api-client/trackResponse';
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
