import { useQueryClient } from '@tanstack/react-query';
import type { InfiniteData } from '@tanstack/react-query';

import type { ListTracksResponse } from '@shared/api-client/types';

export function useIsTrackSaved(title: string, artist: string | null): boolean {
  const queryClient = useQueryClient();
  const normalTitle = title.toLowerCase().trim();
  const normalArtist = (artist ?? '').toLowerCase().trim();

  const homeData = queryClient.getQueryData<ListTracksResponse>(['library-home']);
  if (homeData) {
    return homeData.items.some(
      (t) => t.title.toLowerCase().trim() === normalTitle && t.artist.toLowerCase().trim() === normalArtist,
    );
  }

  const infiniteData = queryClient.getQueryData<InfiniteData<ListTracksResponse>>(['library']);
  if (!infiniteData) return false;

  return infiniteData.pages.some((page) =>
    page.items.some(
      (t) => t.title.toLowerCase().trim() === normalTitle && t.artist.toLowerCase().trim() === normalArtist,
    ),
  );
}
