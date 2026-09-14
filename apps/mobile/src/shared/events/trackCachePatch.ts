import type { InfiniteData, QueryClient, QueryKey } from '@tanstack/react-query';

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

/**
 * Keep each page's `offset` equal to where its first row now sits in the list the
 * cache implies. fetchNextPage derives the next offset from the last page's
 * `offset + items.length`, so a row removed from (or added to) an earlier page must
 * shift every later page, or the next fetch skips (or repeats) a track.
 */
function reflowOffsets(
  before: readonly ListTracksResponse[],
  after: readonly ListTracksResponse[],
): ListTracksResponse[] {
  let shift = 0;
  return after.map((page, i) => {
    const shifted = shift === 0 ? page : { ...page, offset: Math.max(0, page.offset + shift) };
    shift += page.items.length - (before[i]?.items.length ?? page.items.length);
    return shifted;
  });
}

function mapPages(
  prev: TrackPages | undefined,
  mapItems: MapItems,
  adjustTotal: AdjustTotal,
): TrackPages | undefined {
  if (!prev) return prev;
  const pages = prev.pages.map((page) => {
    const items = mapItems(page.items);
    return { ...page, items, total: adjustTotal(page.total, page.items.length, items.length) };
  });
  return { ...prev, pages: reflowOffsets(prev.pages, pages) };
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
        return {
          ...prev,
          items,
          total: policy.listTotal(prev.total, prev.items.length, items.length),
        };
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
      const pages = [{ ...first, items: [track, ...first.items], total: first.total + 1 }, ...rest];
      return { ...prev, pages: reflowOffsets(prev.pages, pages) };
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

/**
 * Where a track sat in one cache entry: the query, the page (0 for unpaged shapes)
 * and every index it occupied. Captured before an optimistic removal so a failed
 * mutation can put the track back exactly where it was.
 */
export interface TrackCachePlacement {
  readonly queryKey: QueryKey;
  readonly shape: TrackCacheShape;
  readonly pageIndex: number;
  readonly indices: readonly number[];
  readonly track: TrackResponse;
}

function indicesOf(items: readonly TrackResponse[], trackId: string): number[] {
  const out: number[] = [];
  items.forEach((t, i) => {
    if (t.id === trackId) out.push(i);
  });
  return out;
}

function unpagedItems(shape: TrackCacheShape, data: unknown): readonly TrackResponse[] {
  return shape === 'flat'
    ? (data as ListTracksResponse).items
    : (data as PlaylistDetailResponse).tracks;
}

export function captureTrackPlacements(
  queryClient: QueryClient,
  trackId: string,
): TrackCachePlacement[] {
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
        record(queryKey, family.shape, 0, unpagedItems(family.shape, data));
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

/**
 * Undo an optimistic removal: put the track back at each captured position,
 * restoring the totals the removal decremented. An entry that already holds the
 * track again (a refetch or server event landed first) is left alone, so the
 * rollback never duplicates a row or clobbers fresher state.
 */
export function restoreTrackPlacements(
  queryClient: QueryClient,
  placements: readonly TrackCachePlacement[],
): void {
  for (const placement of placements) {
    const id = placement.track.id;
    const added = placement.indices.length;
    switch (placement.shape) {
      case 'paged':
        queryClient.setQueryData<TrackPages>(placement.queryKey, (prev) => {
          if (!prev?.pages[placement.pageIndex]) return prev;
          if (prev.pages.some((page) => page.items.some((t) => t.id === id))) return prev;
          const pages = prev.pages.map((page, i) =>
            i === placement.pageIndex
              ? { ...page, items: reinsert(page.items, placement), total: page.total + added }
              : page,
          );
          return { ...prev, pages: reflowOffsets(prev.pages, pages) };
        });
        break;
      case 'flat':
        queryClient.setQueryData<ListTracksResponse>(placement.queryKey, (prev) => {
          if (!prev || prev.items.some((t) => t.id === id)) return prev;
          return { ...prev, items: reinsert(prev.items, placement), total: prev.total + added };
        });
        break;
      case 'playlistDetails':
        queryClient.setQueryData<PlaylistDetailResponse>(placement.queryKey, (prev) => {
          if (!prev || prev.tracks.some((t) => t.id === id)) return prev;
          const tracks = reinsert(prev.tracks, placement);
          return { ...prev, tracks, track_count: tracks.length };
        });
        break;
    }
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
