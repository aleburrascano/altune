/**
 * Look up a track in the React Query library cache by normalized title+artist.
 *
 * Reads the `['library-home']` snapshot. (When the snapshot exists but lacks
 * the track, the answer is "not saved"; when it is absent, the library never
 * loaded and nothing can be concluded.)
 *
 * The single source of truth for the "is this track already in my library"
 * lookup. `useLibraryTrackMatch` (→ the row) is a thin reader over it;
 * `_isTrackInLibraryCache` delegates here too.
 */

import type { QueryClient } from '@tanstack/react-query';

import type { ListTracksResponse, TrackResponse } from '@shared/api-client/types';
import { libraryKeys } from '@shared/lib/query-keys';

/**
 * Pure lookup over already-read cache data. Split out so the reactive hook can
 * subscribe to the query (re-rendering on a setQueryData patch, per F12) and
 * pass its data in, while the imperative wrapper below reads via getQueryData.
 */
export function findTrackInData(
  homeData: ListTracksResponse | undefined,
  title: string,
  artist: string | null,
): TrackResponse | null {
  const normalTitle = title.toLowerCase().trim();
  const normalArtist = (artist ?? '').toLowerCase().trim();
  const matches = (t: TrackResponse): boolean =>
    t.title.toLowerCase().trim() === normalTitle && t.artist.toLowerCase().trim() === normalArtist;

  return homeData?.items.find(matches) ?? null;
}

export function findTrackInLibraryCache(
  queryClient: QueryClient,
  title: string,
  artist: string | null,
): TrackResponse | null {
  return findTrackInData(
    queryClient.getQueryData<ListTracksResponse>(libraryKeys.home),
    title,
    artist,
  );
}
