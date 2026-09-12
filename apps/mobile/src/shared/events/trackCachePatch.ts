import type { InfiniteData, QueryClient } from '@tanstack/react-query';

import type {
  ListTracksResponse,
  PlaylistDetailResponse,
  TrackResponse,
} from '@shared/api-client/types';
import { libraryKeys, playlistKeys } from '@shared/lib/query-keys';

type TrackPages = InfiniteData<ListTracksResponse>;

// The single declaration of every query cache family that stores a Track. Read,
// remove and patch iterate this list (in order); RESYNC_KEYS references it by name.
// Adding a new Track-bearing cache is one entry here, not an edit at five sites.
type TrackCacheShape = 'paged' | 'flat' | 'playlistDetails';

interface TrackCacheFamily {
  readonly prefix: readonly string[];
  readonly shape: TrackCacheShape;
}

export const TRACK_CACHE_FAMILIES = {
  pagedLibrary: { prefix: libraryKeys.tracksPrefix, shape: 'paged' },
  lookup: { prefix: libraryKeys.lookupPrefix, shape: 'flat' },
  featuring: { prefix: libraryKeys.featuringPrefix, shape: 'flat' },
  playlistDetails: { prefix: playlistKeys.details, shape: 'playlistDetails' },
} as const satisfies Record<string, TrackCacheFamily>;

const TRACK_CACHE_FAMILY_LIST: readonly TrackCacheFamily[] = Object.values(TRACK_CACHE_FAMILIES);

type MapItems = (items: TrackResponse[]) => TrackResponse[];
type AdjustTotal = (total: number, before: number, after: number) => number;

interface WritePolicy {
  readonly listTotal: AdjustTotal;
  readonly playlistCount: AdjustTotal;
}

const keepTotal: AdjustTotal = (total) => total;

const REMOVE_POLICY: WritePolicy = {
  listTotal: (total, before, after) => total - (before - after),
  playlistCount: (_count, _before, after) => after,
};

const PATCH_POLICY: WritePolicy = {
  listTotal: keepTotal,
  playlistCount: keepTotal,
};

function mapPages(
  prev: TrackPages | undefined,
  mapItems: MapItems,
  adjustTotal: AdjustTotal,
): TrackPages | undefined {
  if (!prev) return prev;
  return {
    ...prev,
    pages: prev.pages.map((page) => {
      const items = mapItems(page.items);
      return { ...page, items, total: adjustTotal(page.total, page.items.length, items.length) };
    }),
  };
}

function familyItems(shape: TrackCacheShape, data: unknown): readonly TrackResponse[] {
  if (!data) return [];
  switch (shape) {
    case 'paged':
      return (data as TrackPages).pages.flatMap((page) => page.items);
    case 'flat':
      return (data as ListTracksResponse).items;
    case 'playlistDetails':
      return (data as PlaylistDetailResponse).tracks;
  }
}

function writeFamily(
  queryClient: QueryClient,
  family: TrackCacheFamily,
  mapItems: MapItems,
  policy: WritePolicy,
): void {
  switch (family.shape) {
    case 'paged':
      queryClient.setQueriesData<TrackPages>({ queryKey: family.prefix }, (prev) =>
        mapPages(prev, mapItems, policy.listTotal),
      );
      return;
    case 'flat':
      queryClient.setQueriesData<ListTracksResponse>({ queryKey: family.prefix }, (prev) => {
        if (!prev) return prev;
        const items = mapItems(prev.items);
        return { ...prev, items, total: policy.listTotal(prev.total, prev.items.length, items.length) };
      });
      return;
    case 'playlistDetails':
      queryClient.setQueriesData<PlaylistDetailResponse>({ queryKey: family.prefix }, (prev) => {
        if (!prev) return prev;
        const tracks = mapItems(prev.tracks);
        return {
          ...prev,
          tracks,
          track_count: policy.playlistCount(prev.track_count, prev.tracks.length, tracks.length),
        };
      });
      return;
  }
}

export function getTrackFromCaches(
  queryClient: QueryClient,
  trackId: string,
): TrackResponse | undefined {
  for (const family of TRACK_CACHE_FAMILY_LIST) {
    const entries = queryClient.getQueriesData({ queryKey: family.prefix });
    for (const [, data] of entries) {
      const found = familyItems(family.shape, data).find((t) => t.id === trackId);
      if (found) return found;
    }
  }
  return undefined;
}

export function upsertTrackInCaches(queryClient: QueryClient, track: TrackResponse): void {
  queryClient.setQueriesData<TrackPages>(
    { queryKey: TRACK_CACHE_FAMILIES.pagedLibrary.prefix },
    (prev) => {
      if (!prev) return prev;
      const known = prev.pages.some((page) => page.items.some((t) => t.id === track.id));
      if (known) {
        return mapPages(
          prev,
          (items) => items.map((t) => (t.id === track.id ? { ...t, ...track } : t)),
          keepTotal,
        );
      }
      const [first, ...rest] = prev.pages;
      if (!first) return prev;
      return {
        ...prev,
        pages: [{ ...first, items: [track, ...first.items], total: first.total + 1 }, ...rest],
      };
    },
  );
}

export function replaceTrackInCaches(
  queryClient: QueryClient,
  optimisticId: string,
  real: TrackResponse,
): void {
  queryClient.setQueriesData<TrackPages>(
    { queryKey: TRACK_CACHE_FAMILIES.pagedLibrary.prefix },
    (prev) =>
      mapPages(
        prev,
        (items) => dedupById(items.map((t) => (t.id === optimisticId ? real : t))),
        (total, before, after) => total - (before - after),
      ),
  );
}

function dedupById(items: TrackResponse[]): TrackResponse[] {
  const seen = new Set<string>();
  return items.filter((t) => (seen.has(t.id) ? false : (seen.add(t.id), true)));
}

export function removeTrackFromCaches(queryClient: QueryClient, trackId: string): void {
  const drop: MapItems = (items) => items.filter((t) => t.id !== trackId);
  for (const family of TRACK_CACHE_FAMILY_LIST) {
    writeFamily(queryClient, family, drop, REMOVE_POLICY);
  }
}

export function patchTrackInCaches(
  queryClient: QueryClient,
  trackId: string,
  patch: Partial<TrackResponse>,
): void {
  const applyAll: MapItems = (items) =>
    items.map((t) => (t.id === trackId ? { ...t, ...patch } : t));
  for (const family of TRACK_CACHE_FAMILY_LIST) {
    writeFamily(queryClient, family, applyAll, PATCH_POLICY);
  }
}
