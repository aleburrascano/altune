/**
 * useArtistContent — fetch top tracks and albums from an artist.
 *
 * AC#16-18: Fans out to all providers in the artist's sources array for
 * albums (multi-provider merge), and uses the best source for top tracks.
 * Albums are deduped by normalized title (keep highest track_count),
 * then sorted newest-first.
 */

import { useQuery } from '@tanstack/react-query';

import {
  getArtistAlbums,
  getArtistTopTracks,
  type DiscoveryResult,
  type DiscoverySource,
} from '@shared/api-client/discovery';
import { normalizeForDedup } from '../helpers/normalize-for-dedup';

import { dedupAlbumsByTitle, sortByReleaseDateDesc } from '../helpers/artist-content';

type UseArtistContentParams = {
  sources: DiscoverySource[];
  /** Artist name — passed to the backend for MB cross-reference validation. */
  artistName?: string;
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
  sources,
  artistName,
  enabled = true,
}: UseArtistContentParams): UseArtistContentReturn {
  const deezerSource = sources.find((s) => s.provider === 'deezer') ?? null;
  const streamSource = deezerSource ?? sources.find((s) => s.provider === 'lastfm') ?? sources[0] ?? null;

  const {
    data: tracksData,
    isLoading: isLoadingTracksRaw,
    isError: isErrorTracksRaw,
    refetch: refetchTracksRaw,
  } = useQuery({
    queryKey: ['artist-top-tracks', streamSource?.provider ?? '', streamSource?.external_id ?? ''],
    queryFn: () => getArtistTopTracks(streamSource!.provider, streamSource!.external_id, 5),
    enabled: enabled && streamSource !== null,
    staleTime: 1000 * 60 * 30,
  });

  const dzValidated = Boolean(artistName);
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

  const scSource = sources.find((s) => s.provider === 'soundcloud') ?? null;
  const {
    data: scData,
    isLoading: isLoadingSc,
    isError: isErrorSc,
    refetch: refetchSc,
  } = useQuery({
    queryKey: ['artist-albums-sc', scSource?.external_id ?? ''],
    queryFn: () => getArtistAlbums('soundcloud', scSource!.external_id, 100),
    enabled: enabled && scSource !== null,
    staleTime: 1000 * 60 * 30,
  });

  // iTunes is a second mainstream discography source alongside Deezer
  // (docs/providers/itunes.md cap 5). artistName is passed through so the
  // backend applies the same MB consensus validation it does for Deezer.
  const itunesSource = sources.find((s) => s.provider === 'itunes') ?? null;
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

  // One descriptor per discography provider. The useQuery calls above stay
  // explicit (rules of hooks forbid looping them), but every downstream
  // aggregation — merge, loading, the all-failed verdict, refetch — derives
  // from this list. Adding a provider is one query block plus one entry here,
  // not edits scattered across five derivations. Order (deezer, soundcloud,
  // itunes) is preserved so the merge keeps its existing precedence.
  const albumProviders = [
    { source: deezerSource, data: dzData, isLoading: isLoadingDz, isError: isErrorDz, refetch: refetchDz },
    { source: scSource, data: scData, isLoading: isLoadingSc, isError: isErrorSc, refetch: refetchSc },
    { source: itunesSource, data: itData, isLoading: isLoadingIt, isError: isErrorIt, refetch: refetchIt },
  ].filter((p) => p.source !== null);

  const mergedAlbums = dedupAlbumsByTitle(
    albumProviders.flatMap((p) => (p.data?.status === 'ok' ? p.data.items : [])),
  );

  // Back-fill artwork for albums with no image (e.g. SoundCloud sets)
  // from a title-matched album from another provider.
  const artByTitle = new Map<string, string>();
  for (const a of mergedAlbums) {
    if (a.image_url) {
      const key = normalizeForDedup(a.title);
      if (!artByTitle.has(key)) artByTitle.set(key, a.image_url);
    }
  }
  const albumsWithArt = mergedAlbums.map((a) => {
    if (a.image_url) return a;
    const donor = artByTitle.get(normalizeForDedup(a.title));
    return donor ? { ...a, image_url: donor } : a;
  });

  const isLoadingAlbums = albumProviders.some((p) => p.isLoading);
  const albumOutcomes = albumProviders.map(
    (p) => p.isError || (p.data !== undefined && p.data.status !== 'ok'),
  );
  const isErrorAlbums = albumOutcomes.length > 0 && albumOutcomes.every(Boolean);

  // When the backend applied MB validation (artistName was passed), trust its
  // confirmed-first ordering. Only sort by date when no validation occurred.
  const finalAlbums = dzValidated ? albumsWithArt : sortByReleaseDateDesc(albumsWithArt);

  return {
    topTracks: tracksData?.status === 'ok' ? tracksData.items : [],
    albums: finalAlbums,
    isLoadingTracks: isLoadingTracksRaw,
    isLoadingAlbums,
    isErrorTracks:
      isErrorTracksRaw || (tracksData !== undefined && tracksData.status !== 'ok'),
    isErrorAlbums,
    refetchTracks: refetchTracksRaw,
    refetchAlbums: () => { albumProviders.forEach((p) => p.refetch()); },
  };
}
