import type { ListTracksResponse, TrackResponse, PlaylistDetailResponse } from '@shared/apiClient';
import { libraryKeys, playlistKeys } from '@shared/query';

export interface TrackPages {
  pages: ListTracksResponse[];
  pageParams: unknown[];
}

export type TrackCacheShape = 'paged' | 'flat' | 'playlistDetails';

export type TrackCacheValue = TrackPages | ListTracksResponse | PlaylistDetailResponse;

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

export type MapItems = (items: TrackResponse[], pageIndex: number) => TrackResponse[];

type AdjustTotal = (total: number, before: number, after: number) => number;

export interface WritePolicy {
  readonly listTotal: AdjustTotal;
  readonly playlistCount: AdjustTotal;
}

function keepTotal(total: number): number {
  return total;
}

export const RESIZE_POLICY: WritePolicy = {
  listTotal: (total, before, after) => total - (before - after),
  playlistCount: (_count, _before, after) => after,
};

export const PATCH_POLICY: WritePolicy = {
  listTotal: keepTotal,
  playlistCount: keepTotal,
};

type Pages = readonly ListTracksResponse[];

function shiftOffset(page: ListTracksResponse, shift: number): ListTracksResponse {
  return shift === 0 ? page : { ...page, offset: Math.max(0, page.offset + shift) };
}

export function reflowOffsets(before: Pages, after: Pages): ListTracksResponse[] {
  let shift = 0;
  return after.map((page, i) => {
    const shifted = shiftOffset(page, shift);
    shift += page.items.length - (before[i]?.items.length ?? page.items.length);
    return shifted;
  });
}

const NO_TRACKS: readonly TrackResponse[] = [];

export interface ShapeAccessor<D extends TrackCacheValue> {
  items(data: D | undefined): readonly TrackResponse[];
  write(prev: D | undefined, mapItems: MapItems, policy: WritePolicy): D | undefined;
}

function resizePage(
  page: ListTracksResponse,
  pageIndex: number,
  mapItems: MapItems,
  policy: WritePolicy,
): ListTracksResponse {
  const items = mapItems(page.items, pageIndex);
  const total = policy.listTotal(page.total, page.items.length, items.length);
  return { ...page, items, total };
}

export const pagedAccessor: ShapeAccessor<TrackPages> = {
  items: (data) => (data ? data.pages.flatMap((page) => page.items) : NO_TRACKS),
  write: (prev, mapItems, policy) => {
    if (!prev) return prev;
    const pages = prev.pages.map((page, i) => resizePage(page, i, mapItems, policy));
    return { ...prev, pages: reflowOffsets(prev.pages, pages) };
  },
};

const flatAccessor: ShapeAccessor<ListTracksResponse> = {
  items: (data) => (data ? data.items : NO_TRACKS),
  write: (prev, mapItems, policy) => {
    if (!prev) return prev;
    const items = mapItems(prev.items, 0);
    return { ...prev, items, total: policy.listTotal(prev.total, prev.items.length, items.length) };
  },
};

const playlistDetailsAccessor: ShapeAccessor<PlaylistDetailResponse> = {
  items: (data) => (data ? data.tracks : NO_TRACKS),
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

export const TRACK_SHAPES: Record<TrackCacheShape, ShapeAccessor<TrackCacheValue>> = {
  paged: pagedAccessor,
  flat: flatAccessor,
  playlistDetails: playlistDetailsAccessor,
};

export function updaterFor(shape: TrackCacheShape, mapItems: MapItems, policy: WritePolicy) {
  return (prev: TrackCacheValue | undefined) => TRACK_SHAPES[shape].write(prev, mapItems, policy);
}
