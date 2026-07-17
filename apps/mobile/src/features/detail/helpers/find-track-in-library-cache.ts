/**
 * Look up a track in the React Query library cache by normalized title+artist.
 *
 * Reads the `['library-home']` snapshot first; only when that key is absent does
 * it scan the `['library']` infinite-query pages. (When the home snapshot exists
 * but lacks the track, the answer is "not saved" — we do not fall through.)
 *
 * The single source of truth for the "is this track already in my library"
 * lookup. `useLibraryTrackMatch` (→ the row) is a thin reader over it;
 * `_isTrackInLibraryCache` delegates here too.
 */

import type { InfiniteData, QueryClient } from '@tanstack/react-query';

import type { ListTracksResponse, TrackResponse } from '@shared/api-client/types';

/**
 * Pure lookup over already-read cache data. Split out so the reactive hooks can
 * subscribe to the queries (re-rendering on a setQueryData patch, per F12) and
 * pass their data in, while the imperative wrapper below reads via getQueryData.
 */
export function findTrackInData(
  homeData: ListTracksResponse | undefined,
  infiniteData: InfiniteData<ListTracksResponse> | undefined,
  title: string,
  artist: string | null,
): TrackResponse | null {
  const normalTitle = title.toLowerCase().trim();
  const normalArtist = (artist ?? '').toLowerCase().trim();
  const matches = (t: TrackResponse): boolean =>
    t.title.toLowerCase().trim() === normalTitle && t.artist.toLowerCase().trim() === normalArtist;

  if (homeData) {
    return homeData.items.find(matches) ?? null;
  }

  if (!infiniteData) return null;

  for (const page of infiniteData.pages) {
    const match = page.items.find(matches);
    if (match) return match;
  }
  return null;
}

export function findTrackInLibraryCache(
  queryClient: QueryClient,
  title: string,
  artist: string | null,
): TrackResponse | null {
  return findTrackInData(
    queryClient.getQueryData<ListTracksResponse>(['library-home']),
    queryClient.getQueryData<InfiniteData<ListTracksResponse>>(['library']),
    title,
    artist,
  );
}
