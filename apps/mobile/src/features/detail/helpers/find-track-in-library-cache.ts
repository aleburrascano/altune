import type { QueryClient } from '@tanstack/react-query';

import type { ListTracksResponse, TrackResponse } from '@shared/api-client/types';
import { libraryKeys } from '@shared/lib/query-keys';

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
