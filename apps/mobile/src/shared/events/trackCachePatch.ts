import type { InfiniteData, QueryClient } from '@tanstack/react-query';

import type {
  ListTracksResponse,
  PlaylistDetailResponse,
  TrackResponse,
} from '@shared/api-client/types';
import { libraryKeys, playlistKeys } from '@shared/lib/query-keys';

type TrackPages = InfiniteData<ListTracksResponse>;

const keepTotal = (total: number): number => total;

function mapPages(
  prev: TrackPages | undefined,
  mapItems: (items: TrackResponse[]) => TrackResponse[],
  adjustTotal: (total: number, before: number, after: number) => number,
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

function mapFlatList(
  prev: ListTracksResponse | undefined,
  mapItems: (items: TrackResponse[]) => TrackResponse[],
): ListTracksResponse | undefined {
  if (!prev) return prev;
  const items = mapItems(prev.items);
  return { ...prev, items, total: prev.total - (prev.items.length - items.length) };
}

export function getTrackFromCaches(
  queryClient: QueryClient,
  trackId: string,
): TrackResponse | undefined {
  const paged = queryClient.getQueriesData<TrackPages>({ queryKey: libraryKeys.tracksPrefix });
  for (const [, data] of paged) {
    for (const page of data?.pages ?? []) {
      const found = page.items.find((t) => t.id === trackId);
      if (found) return found;
    }
  }

  for (const prefix of [libraryKeys.lookupPrefix, libraryKeys.featuringPrefix]) {
    const lists = queryClient.getQueriesData<ListTracksResponse>({ queryKey: prefix });
    for (const [, list] of lists) {
      const found = list?.items.find((t) => t.id === trackId);
      if (found) return found;
    }
  }

  const playlists = queryClient.getQueriesData<PlaylistDetailResponse>({
    queryKey: playlistKeys.details,
  });
  for (const [, detail] of playlists) {
    const found = detail?.tracks.find((t) => t.id === trackId);
    if (found) return found;
  }
  return undefined;
}

export function upsertTrackInCaches(queryClient: QueryClient, track: TrackResponse): void {
  queryClient.setQueriesData<TrackPages>({ queryKey: libraryKeys.tracksPrefix }, (prev) => {
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
  });
}

export function replaceTrackInCaches(
  queryClient: QueryClient,
  optimisticId: string,
  real: TrackResponse,
): void {
  queryClient.setQueriesData<TrackPages>({ queryKey: libraryKeys.tracksPrefix }, (prev) =>
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
  const drop = (items: TrackResponse[]): TrackResponse[] => items.filter((t) => t.id !== trackId);

  queryClient.setQueriesData<TrackPages>({ queryKey: libraryKeys.tracksPrefix }, (prev) =>
    mapPages(prev, drop, (total, before, after) => total - (before - after)),
  );

  for (const prefix of [libraryKeys.lookupPrefix, libraryKeys.featuringPrefix]) {
    queryClient.setQueriesData<ListTracksResponse>({ queryKey: prefix }, (prev) =>
      mapFlatList(prev, drop),
    );
  }

  queryClient.setQueriesData<PlaylistDetailResponse>({ queryKey: playlistKeys.details }, (prev) => {
    if (!prev) return prev;
    const tracks = drop(prev.tracks);
    return { ...prev, tracks, track_count: tracks.length };
  });
}

export function patchTrackInCaches(
  queryClient: QueryClient,
  trackId: string,
  patch: Partial<TrackResponse>,
): void {
  const apply = (t: TrackResponse): TrackResponse => (t.id === trackId ? { ...t, ...patch } : t);
  const applyAll = (items: TrackResponse[]): TrackResponse[] => items.map(apply);

  queryClient.setQueriesData<TrackPages>({ queryKey: libraryKeys.tracksPrefix }, (prev) =>
    mapPages(prev, applyAll, keepTotal),
  );

  for (const prefix of [libraryKeys.lookupPrefix, libraryKeys.featuringPrefix]) {
    queryClient.setQueriesData<ListTracksResponse>({ queryKey: prefix }, (prev) =>
      prev ? { ...prev, items: applyAll(prev.items) } : prev,
    );
  }

  queryClient.setQueriesData<PlaylistDetailResponse>({ queryKey: playlistKeys.details }, (prev) =>
    prev ? { ...prev, tracks: applyAll(prev.tracks) } : prev,
  );
}
