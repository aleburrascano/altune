import { useQuery, useQueryClient } from '@tanstack/react-query';

import { getTracks } from '@shared/api-client/tracks';
import type { ListTracksResponse, TrackResponse } from '@shared/api-client/types';
import type { DiscoveryResult } from '@shared/api-client/discovery';

function useLibraryHomeData(): ListTracksResponse | undefined {
  const queryClient = useQueryClient();
  const cached = queryClient.getQueryData<ListTracksResponse>(['library-home']);

  const { data } = useQuery({
    queryKey: ['library-home'],
    queryFn: () => getTracks({ limit: 2000, offset: 0 }),
    enabled: cached == null,
    staleTime: 30_000,
  });

  return cached ?? data;
}

export function useLibraryTracksForAlbum(albumTitle: string, artist: string | null): TrackResponse[] {
  const homeData = useLibraryHomeData();
  if (!homeData) return [];

  const albumNorm = albumTitle.toLowerCase().trim();
  const artistNorm = (artist ?? '').toLowerCase().trim();

  return homeData.items.filter((t) => {
    const tAlbum = (t.album ?? '').toLowerCase().trim();
    const tArtist = (t.album_artist ?? t.artist).toLowerCase().trim();
    return tAlbum === albumNorm && (artistNorm === '' || tArtist === artistNorm);
  });
}

export function useLibraryTracksForArtist(artistName: string): TrackResponse[] {
  const homeData = useLibraryHomeData();
  if (!homeData) return [];

  const artistNorm = artistName.toLowerCase().trim();
  return homeData.items.filter(
    (t) => t.artist.toLowerCase().trim() === artistNorm,
  );
}

export function libraryTrackToDiscoveryResult(track: TrackResponse): DiscoveryResult {
  return {
    kind: 'track',
    title: track.title,
    subtitle: track.artist,
    image_url: track.artwork_url ?? null,
    confidence: 'high',
    sources: [],
    extras: {
      ...(track.album != null ? { album: track.album } : {}),
      ...(track.duration_seconds != null ? { duration_seconds: track.duration_seconds } : {}),
      ...(track.track_number != null ? { track_position: track.track_number } : {}),
      acquisition_status: track.acquisition_status,
      track_id: track.id,
    },
  };
}
