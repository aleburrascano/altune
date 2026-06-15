import { useQueryClient } from '@tanstack/react-query';
import type { InfiniteData } from '@tanstack/react-query';

import type { ListTracksResponse, TrackResponse } from '@shared/api-client/types';

export function useLibraryTrackMatch(title: string, artist: string | null): TrackResponse | null {
  const queryClient = useQueryClient();
  const normalTitle = title.toLowerCase().trim();
  const normalArtist = (artist ?? '').toLowerCase().trim();

  const homeData = queryClient.getQueryData<ListTracksResponse>(['library-home']);
  if (homeData) {
    return homeData.items.find(
      (t) => t.title.toLowerCase().trim() === normalTitle && t.artist.toLowerCase().trim() === normalArtist,
    ) ?? null;
  }

  const infiniteData = queryClient.getQueryData<InfiniteData<ListTracksResponse>>(['library']);
  if (!infiniteData) return null;

  for (const page of infiniteData.pages) {
    const match = page.items.find(
      (t) => t.title.toLowerCase().trim() === normalTitle && t.artist.toLowerCase().trim() === normalArtist,
    );
    if (match) return match;
  }

  return null;
}
