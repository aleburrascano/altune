/**
 * useArtistContent — fetch top tracks and albums from an artist.
 *
 * AC#17-18: Fetches both in parallel from the first source in the artist's
 * sources array. Cached per (provider, external_id) for the session.
 */

import { useQuery } from '@tanstack/react-query';

import {
  getArtistAlbums,
  getArtistTopTracks,
  type DiscoveryResult,
} from '@shared/api-client/discovery';

function getReleaseSortKey(album: DiscoveryResult): string | null {
  // Deezer uses release_date (YYYY-MM-DD), MB uses year (just YYYY)
  const releaseDate = album.extras['release_date'];
  if (typeof releaseDate === 'string') return releaseDate;
  const year = album.extras['year'];
  if (typeof year === 'string') return year;
  if (typeof year === 'number') return String(year);
  return null;
}

function sortByReleaseDateDesc(albums: DiscoveryResult[]): DiscoveryResult[] {
  return [...albums].sort((a, b) => {
    const dateA = getReleaseSortKey(a);
    const dateB = getReleaseSortKey(b);
    if (dateA === null) return 1;
    if (dateB === null) return -1;
    return dateB.localeCompare(dateA);
  });
}

type UseArtistContentParams = {
  provider: string;
  externalId: string;
  enabled?: boolean;
};

type UseArtistContentReturn = {
  topTracks: DiscoveryResult[];
  albums: DiscoveryResult[];
  isLoadingTracks: boolean;
  isLoadingAlbums: boolean;
  isErrorTracks: boolean;
  isErrorAlbums: boolean;
  refetchTracks: () => void;
  refetchAlbums: () => void;
};

export function useArtistContent({
  provider,
  externalId,
  enabled = true,
}: UseArtistContentParams): UseArtistContentReturn {
  const tracksQuery = useQuery({
    queryKey: ['artist-top-tracks', provider, externalId],
    queryFn: () => getArtistTopTracks(provider, externalId, 5),
    enabled,
    staleTime: 1000 * 60 * 30, // 30 minutes
  });

  const albumsQuery = useQuery({
    queryKey: ['artist-albums', provider, externalId],
    queryFn: () => getArtistAlbums(provider, externalId, 10),
    enabled,
    staleTime: 1000 * 60 * 30, // 30 minutes
  });

  const rawAlbums = albumsQuery.data?.items ?? [];

  return {
    topTracks: tracksQuery.data?.items ?? [],
    albums: sortByReleaseDateDesc(rawAlbums),
    isLoadingTracks: tracksQuery.isLoading,
    isLoadingAlbums: albumsQuery.isLoading,
    isErrorTracks: tracksQuery.isError || tracksQuery.data?.status === 'error',
    isErrorAlbums: albumsQuery.isError || albumsQuery.data?.status === 'error',
    refetchTracks: tracksQuery.refetch,
    refetchAlbums: albumsQuery.refetch,
  };
}
