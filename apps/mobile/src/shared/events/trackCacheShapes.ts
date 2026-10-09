import type { InfiniteData, QueryClient, QueryFilters } from '@tanstack/react-query';

import type { ListTracksResponse, TrackResponse } from '@shared/api-client/trackResponse';
import type { PlaylistDetailResponse } from '@shared/api-client/playlistResponse';
import { libraryKeys, playlistKeys } from '@shared/query/queryKeys';

export type TrackPages = InfiniteData<ListTracksResponse>;

export type TrackCacheShape = 'paged' | 'flat' | 'playlistDetails';

export interface TrackCacheFamily {
  readonly prefix: readonly string[];
  readonly shape: TrackCacheShape;
}

export const TRACK_CACHE_FAMILIES = {
  pagedLibrary: { prefix: libraryKeys.tracksPrefix, shape: 'paged' },
  lookup: { prefix: libraryKeys.lookupPrefix, shape: 'flat' },
  featuring: { prefix: libraryKeys.featuringPrefix, shape: 'flat' },
  playlistDetails: { prefix: playlistKeys.details, shape: 'playlistDetails' },
} as const satisfies Record<string, TrackCacheFamily>;

export const TRACK_CACHE_FAMILY_LIST: readonly TrackCacheFamily[] =
  Object.values(TRACK_CACHE_FAMILIES);

/** Maps one list of items; `pageIndex` is the page being mapped (0 for unpaged shapes). */
export type MapItems = (items: TrackResponse[], pageIndex: number) => TrackResponse[];

type AdjustTotal = (total: number, before: number, after: number) => number;

export interface WritePolicy {
  readonly listTotal: AdjustTotal;
  readonly playlistCount: AdjustTotal;
}

const keepTotal: AdjustTotal = (total) => total;

/** The count follows the items: it shrinks or grows by however many the mapper added or dropped. */
export const RESIZE_POLICY: WritePolicy = {
  listTotal: (total, before, after) => total - (before - after),
  playlistCount: (_count, _before, after) => after,
};

export const PATCH_POLICY: WritePolicy = {
  listTotal: keepTotal,
  playlistCount: keepTotal,
};

export function reflowOffsets(
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

export interface ShapeAccessor<D> {
  readonly items: (data: D | undefined) => readonly TrackResponse[];
  readonly write: (prev: D | undefined, mapItems: MapItems, policy: WritePolicy) => D | undefined;
}

export const pagedAccessor: ShapeAccessor<TrackPages> = {
  items: (data) => data?.pages.flatMap((page) => page.items) ?? [],
  write: (prev, mapItems, policy) => {
    if (!prev) return prev;
    const pages = prev.pages.map((page, i) => {
      const items = mapItems(page.items, i);
      return {
        ...page,
        items,
        total: policy.listTotal(page.total, page.items.length, items.length),
      };
    });
    return { ...prev, pages: reflowOffsets(prev.pages, pages) };
  },
};

const flatAccessor: ShapeAccessor<ListTracksResponse> = {
  items: (data) => data?.items ?? [],
  write: (prev, mapItems, policy) => {
    if (!prev) return prev;
    const items = mapItems(prev.items, 0);
    return { ...prev, items, total: policy.listTotal(prev.total, prev.items.length, items.length) };
  },
};

const playlistDetailsAccessor: ShapeAccessor<PlaylistDetailResponse> = {
  items: (data) => data?.tracks ?? [],
  write: (prev, mapItems, policy) => {
    if (!prev) return prev;
    const tracks = mapItems(prev.tracks, 0);
    return {
      ...prev,
      tracks,
      track_count: policy.playlistCount(prev.track_count, prev.tracks.length, tracks.length),
    };
  },
};

// Cache data is `unknown` at the query-client boundary; each accessor owns the cast to its shape.
export const TRACK_SHAPES: Record<TrackCacheShape, ShapeAccessor<unknown>> = {
  paged: pagedAccessor as ShapeAccessor<unknown>,
  flat: flatAccessor as ShapeAccessor<unknown>,
  playlistDetails: playlistDetailsAccessor as ShapeAccessor<unknown>,
};

export function writeFamily(
  queryClient: QueryClient,
  family: TrackCacheFamily,
  filters: QueryFilters,
  mapItems: MapItems,
  policy: WritePolicy,
): void {
  const accessor = TRACK_SHAPES[family.shape];
  queryClient.setQueriesData<unknown>(filters, (prev: unknown) =>
    accessor.write(prev, mapItems, policy),
  );
}
