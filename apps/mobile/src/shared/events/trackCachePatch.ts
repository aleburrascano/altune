/**
 * patchTrackInCaches — the single source of truth for a track's live state.
 *
 * Applies a partial update to a track wherever it is cached (library-home
 * snapshot, library infinite query, every playlist detail), keyed by id. This
 * is what makes a backend acquisition event flip every screen at once, instead
 * of one screen's query invalidating while others show stale 'pending'.
 */

import type { InfiniteData, QueryClient } from '@tanstack/react-query';

import type {
  ListTracksResponse,
  PlaylistDetailResponse,
  TrackResponse,
} from '@shared/api-client/types';

/**
 * Finds a track by id across the same caches patchTrackInCaches writes, so the
 * download UI can snapshot display metadata (title/artist/artwork) for an event
 * that carries only a track_id. Returns undefined when the track isn't cached
 * anywhere (e.g. a save from Detail before the library was ever loaded).
 */
export function getTrackFromCaches(
  queryClient: QueryClient,
  trackId: string,
): TrackResponse | undefined {
  const home = queryClient.getQueryData<ListTracksResponse>(['library-home']);
  const inHome = home?.items.find((t) => t.id === trackId);
  if (inHome) return inHome;

  const infinite = queryClient.getQueryData<InfiniteData<ListTracksResponse>>(['library']);
  for (const page of infinite?.pages ?? []) {
    const found = page.items.find((t) => t.id === trackId);
    if (found) return found;
  }

  const playlists = queryClient.getQueriesData<PlaylistDetailResponse>({ queryKey: ['playlist'] });
  for (const [, detail] of playlists) {
    const found = detail?.tracks.find((t) => t.id === trackId);
    if (found) return found;
  }
  return undefined;
}

/**
 * Inserts (or refreshes) a full track into the library caches from a
 * track_added_to_library event (F10), so a receiving device renders the new row
 * instantly instead of refetching. Prepended (most-recent-first; the screens
 * re-sort). No-op on caches that aren't loaded yet — they'll fetch it fresh.
 * Playlists are untouched: a newly-saved library track is in no playlist.
 */
export function upsertTrackInCaches(queryClient: QueryClient, track: TrackResponse): void {
  queryClient.setQueryData<ListTracksResponse>(['library-home'], (prev) => {
    if (!prev) return prev;
    if (prev.items.some((t) => t.id === track.id)) {
      return { ...prev, items: prev.items.map((t) => (t.id === track.id ? { ...t, ...track } : t)) };
    }
    return { ...prev, items: [track, ...prev.items], total: prev.total + 1 };
  });

  queryClient.setQueryData<InfiniteData<ListTracksResponse>>(['library'], (prev) => {
    if (!prev || prev.pages.length === 0) return prev;
    if (prev.pages.some((p) => p.items.some((t) => t.id === track.id))) {
      return {
        ...prev,
        pages: prev.pages.map((p) => ({
          ...p,
          items: p.items.map((t) => (t.id === track.id ? { ...t, ...track } : t)),
        })),
      };
    }
    const [first, ...rest] = prev.pages;
    return {
      ...prev,
      pages: [{ ...first!, items: [track, ...first!.items], total: first!.total + 1 }, ...rest],
    };
  });
}

/**
 * Removes a track by id from every cache that can hold it (F11) — the library
 * home snapshot, the library infinite query, and every playlist detail — so a
 * delete (incl. from another device) drops the row instantly instead of firing
 * three refetches.
 */
export function removeTrackFromCaches(queryClient: QueryClient, trackId: string): void {
  queryClient.setQueryData<ListTracksResponse>(['library-home'], (prev) => {
    if (!prev) return prev;
    const items = prev.items.filter((t) => t.id !== trackId);
    return { ...prev, items, total: prev.total - (items.length < prev.items.length ? 1 : 0) };
  });

  queryClient.setQueryData<InfiniteData<ListTracksResponse>>(['library'], (prev) =>
    prev
      ? { ...prev, pages: prev.pages.map((p) => ({ ...p, items: p.items.filter((t) => t.id !== trackId) })) }
      : prev,
  );

  queryClient.setQueriesData<PlaylistDetailResponse>({ queryKey: ['playlist'] }, (prev) =>
    prev ? { ...prev, tracks: prev.tracks.filter((t) => t.id !== trackId) } : prev,
  );
}

export function patchTrackInCaches(
  queryClient: QueryClient,
  trackId: string,
  patch: Partial<TrackResponse>,
): void {
  const apply = (t: TrackResponse): TrackResponse => (t.id === trackId ? { ...t, ...patch } : t);

  queryClient.setQueryData<ListTracksResponse>(['library-home'], (prev) =>
    prev ? { ...prev, items: prev.items.map(apply) } : prev,
  );

  queryClient.setQueryData<InfiniteData<ListTracksResponse>>(['library'], (prev) =>
    prev ? { ...prev, pages: prev.pages.map((p) => ({ ...p, items: p.items.map(apply) })) } : prev,
  );

  queryClient.setQueriesData<PlaylistDetailResponse>({ queryKey: ['playlist'] }, (prev) =>
    prev ? { ...prev, tracks: prev.tracks.map(apply) } : prev,
  );
}
