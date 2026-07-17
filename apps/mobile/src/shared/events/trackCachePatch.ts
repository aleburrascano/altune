/**
 * patchTrackInCaches — the single source of truth for a track's live state.
 *
 * Applies a partial update to a track wherever it is cached (library-home
 * snapshot, every "featuring" list, every playlist detail), keyed by id. This
 * is what makes a backend acquisition event flip every screen at once, instead
 * of one screen's query invalidating while others show stale 'pending'.
 */

import type { QueryClient } from '@tanstack/react-query';

import type {
  ListTracksResponse,
  PlaylistDetailResponse,
  TrackResponse,
} from '@shared/api-client/types';
import { libraryKeys, playlistKeys } from '@shared/lib/query-keys';

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
  const home = queryClient.getQueryData<ListTracksResponse>(libraryKeys.home);
  const inHome = home?.items.find((t) => t.id === trackId);
  if (inHome) return inHome;

  const featuring = queryClient.getQueriesData<ListTracksResponse>({ queryKey: libraryKeys.featuringPrefix });
  for (const [, list] of featuring) {
    const found = list?.items.find((t) => t.id === trackId);
    if (found) return found;
  }

  const playlists = queryClient.getQueriesData<PlaylistDetailResponse>({ queryKey: playlistKeys.details });
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
 * Playlists and featuring lists are untouched: a newly-saved library track is in
 * no playlist, and matching it to a featuring list needs its featured_artists
 * (applyServerEvent invalidates the featuring family for that path instead).
 */
export function upsertTrackInCaches(queryClient: QueryClient, track: TrackResponse): void {
  queryClient.setQueryData<ListTracksResponse>(libraryKeys.home, (prev) => {
    if (!prev) return prev;
    if (prev.items.some((t) => t.id === track.id)) {
      return { ...prev, items: prev.items.map((t) => (t.id === track.id ? { ...t, ...track } : t)) };
    }
    return { ...prev, items: [track, ...prev.items], total: prev.total + 1 };
  });
}

/**
 * Removes a track by id from every cache that can hold it (F11) — the library
 * home snapshot, every "featuring" list, and every playlist detail — so a
 * delete (incl. from another device, or from the FeaturingScreen's own menu)
 * drops the row instantly instead of firing refetches.
 */
export function removeTrackFromCaches(queryClient: QueryClient, trackId: string): void {
  const removeFromList = (prev: ListTracksResponse | undefined): ListTracksResponse | undefined => {
    if (!prev) return prev;
    const items = prev.items.filter((t) => t.id !== trackId);
    return { ...prev, items, total: prev.total - (items.length < prev.items.length ? 1 : 0) };
  };

  queryClient.setQueryData<ListTracksResponse>(libraryKeys.home, removeFromList);
  queryClient.setQueriesData<ListTracksResponse>({ queryKey: libraryKeys.featuringPrefix }, removeFromList);

  queryClient.setQueriesData<PlaylistDetailResponse>({ queryKey: playlistKeys.details }, (prev) =>
    prev ? { ...prev, tracks: prev.tracks.filter((t) => t.id !== trackId) } : prev,
  );
}

export function patchTrackInCaches(
  queryClient: QueryClient,
  trackId: string,
  patch: Partial<TrackResponse>,
): void {
  const apply = (t: TrackResponse): TrackResponse => (t.id === trackId ? { ...t, ...patch } : t);
  const patchList = (prev: ListTracksResponse | undefined): ListTracksResponse | undefined =>
    prev ? { ...prev, items: prev.items.map(apply) } : prev;

  queryClient.setQueryData<ListTracksResponse>(libraryKeys.home, patchList);
  queryClient.setQueriesData<ListTracksResponse>({ queryKey: libraryKeys.featuringPrefix }, patchList);

  queryClient.setQueriesData<PlaylistDetailResponse>({ queryKey: playlistKeys.details }, (prev) =>
    prev ? { ...prev, tracks: prev.tracks.map(apply) } : prev,
  );
}
