import { useQuery } from '@tanstack/react-query';

import { getArtistAlbums, getArtistTopTracks } from '@shared/api-client/enrichment';
import type { DiscoveryResult, DiscoverySource } from '@shared/api-client/discovery';
import {
  backfillAlbumArt,
  dedupAlbumsByTitle,
  dedupeTracksByTitle,
  sortByReleaseDateDesc,
} from '../helpers/artist-content';

type UseArtistContentParams = {
  sources: DiscoverySource[];
  artistName?: string;
  mbid?: string;
  enabled?: boolean;
};

type ArtistTopTracksResult = {
  topTracks: DiscoveryResult[];
  isLoadingTracks: boolean;
  isErrorTracks: boolean;
  refetchTracks: () => void;
};

type ArtistAlbumsResult = {
  albums: DiscoveryResult[];
  isLoadingAlbums: boolean;
  isErrorAlbums: boolean;
  refetchAlbums: () => void;
};

type UseArtistContentReturn = ArtistTopTracksResult & ArtistAlbumsResult;

export function useArtistTopTracks({
  sources,
  mbid,
  artistName,
  enabled = true,
}: Pick<
  UseArtistContentParams,
  'sources' | 'mbid' | 'artistName' | 'enabled'
>): ArtistTopTracksResult {
  const deezerSource = sources.find((s) => s.provider === 'deezer') ?? null;
  const scSource = sources.find((s) => s.provider === 'soundcloud') ?? null;

  const {
    data: dzTracksData,
    isLoading: isLoadingDzTracks,
    isError: isErrorDzTracks,
    refetch: refetchDzTracks,
  } = useQuery({
    queryKey: ['artist-top-tracks-dz', deezerSource?.external_id ?? '', artistName ?? ''],
    queryFn: () => getArtistTopTracks('deezer', deezerSource!.external_id, 5, artistName),
    enabled: enabled && deezerSource !== null,
    staleTime: 1000 * 60 * 30,
  });
  const {
    data: scTracksData,
    isLoading: isLoadingScTracks,
    isError: isErrorScTracks,
    refetch: refetchScTracks,
  } = useQuery({
    queryKey: ['artist-top-tracks-sc', scSource?.external_id ?? ''],
    queryFn: () => getArtistTopTracks('soundcloud', scSource!.external_id, 5),
    enabled: enabled && scSource !== null,
    staleTime: 1000 * 60 * 30,
  });

  const {
    data: lfmTracksData,
    isLoading: isLoadingLfmTracks,
    isError: isErrorLfmTracks,
    refetch: refetchLfmTracks,
  } = useQuery({
    queryKey: ['artist-top-tracks-lfm', mbid ?? ''],
    queryFn: () => getArtistTopTracks('lastfm', mbid!, 5),
    enabled: enabled && Boolean(mbid),
    staleTime: 1000 * 60 * 30,
  });

  const topTrackProviders = [
    {
      source: deezerSource,
      data: dzTracksData,
      isLoading: isLoadingDzTracks,
      isError: isErrorDzTracks,
      refetch: refetchDzTracks,
    },
    {
      source: scSource,
      data: scTracksData,
      isLoading: isLoadingScTracks,
      isError: isErrorScTracks,
      refetch: refetchScTracks,
    },
    {
      source: mbid ? { provider: 'lastfm', external_id: mbid, url: '' } : null,
      data: lfmTracksData,
      isLoading: isLoadingLfmTracks,
      isError: isErrorLfmTracks,
      refetch: refetchLfmTracks,
    },
  ].filter((p) => p.source !== null);

  const mergedTopTracks = dedupeTracksByTitle(
    topTrackProviders.flatMap((p) => (p.data?.status === 'ok' ? p.data.items : [])),
  ).slice(0, 5);

  const isLoadingTracks = topTrackProviders.some((p) => p.isLoading);
  const trackOutcomes = topTrackProviders.map(
    (p) => p.isError || (p.data !== undefined && p.data.status !== 'ok'),
  );
  const isErrorTracks = trackOutcomes.length > 0 && trackOutcomes.every(Boolean);

  return {
    topTracks: mergedTopTracks,
    isLoadingTracks,
    isErrorTracks,
    refetchTracks: () => {
      topTrackProviders.forEach((p) => p.refetch());
    },
  };
}

export function useArtistAlbums({
  sources,
  artistName,
  enabled = true,
}: Pick<UseArtistContentParams, 'sources' | 'artistName' | 'enabled'>): ArtistAlbumsResult {
  const deezerSource = sources.find((s) => s.provider === 'deezer') ?? null;
  const scSource = sources.find((s) => s.provider === 'soundcloud') ?? null;
  const itunesSource = sources.find((s) => s.provider === 'itunes') ?? null;

  const {
    data: dzData,
    isLoading: isLoadingDz,
    isError: isErrorDz,
    refetch: refetchDz,
  } = useQuery({
    queryKey: ['artist-albums-dz', deezerSource?.external_id ?? '', artistName ?? ''],
    queryFn: () => getArtistAlbums('deezer', deezerSource!.external_id, 100, artistName),
    enabled: enabled && deezerSource !== null,
    staleTime: 1000 * 60 * 30,
  });

  const {
    data: scData,
    isLoading: isLoadingSc,
    isError: isErrorSc,
    refetch: refetchSc,
  } = useQuery({
    queryKey: ['artist-albums-sc', scSource?.external_id ?? '', artistName ?? ''],
    queryFn: () => getArtistAlbums('soundcloud', scSource!.external_id, 100, artistName),
    enabled: enabled && scSource !== null,
    staleTime: 1000 * 60 * 30,
  });

  const {
    data: itData,
    isLoading: isLoadingIt,
    isError: isErrorIt,
    refetch: refetchIt,
  } = useQuery({
    queryKey: ['artist-albums-it', itunesSource?.external_id ?? '', artistName ?? ''],
    queryFn: () => getArtistAlbums('itunes', itunesSource!.external_id, 100, artistName),
    enabled: enabled && itunesSource !== null,
    staleTime: 1000 * 60 * 30,
  });

  const albumProviders = [
    {
      source: deezerSource,
      data: dzData,
      isLoading: isLoadingDz,
      isError: isErrorDz,
      refetch: refetchDz,
    },
    {
      source: scSource,
      data: scData,
      isLoading: isLoadingSc,
      isError: isErrorSc,
      refetch: refetchSc,
    },
    {
      source: itunesSource,
      data: itData,
      isLoading: isLoadingIt,
      isError: isErrorIt,
      refetch: refetchIt,
    },
  ].filter((p) => p.source !== null);

  const mergedAlbums = dedupAlbumsByTitle(
    albumProviders.flatMap((p) => (p.data?.status === 'ok' ? p.data.items : [])),
  );

  const albumsWithArt = backfillAlbumArt(mergedAlbums);

  const isLoadingAlbums = albumProviders.some((p) => p.isLoading);
  const albumOutcomes = albumProviders.map(
    (p) => p.isError || (p.data !== undefined && p.data.status !== 'ok'),
  );
  const isErrorAlbums = albumOutcomes.length > 0 && albumOutcomes.every(Boolean);

  const finalAlbums = sortByReleaseDateDesc(albumsWithArt);

  return {
    albums: finalAlbums,
    isLoadingAlbums,
    isErrorAlbums,
    refetchAlbums: () => {
      albumProviders.forEach((p) => p.refetch());
    },
  };
}

export function useArtistContent(params: UseArtistContentParams): UseArtistContentReturn {
  return {
    ...useArtistTopTracks(params),
    ...useArtistAlbums(params),
  };
}
