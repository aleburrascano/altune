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

  const dzFailed = isErrorDz || (dzData !== undefined && dzData.status !== 'ok');
  const scFailed = isErrorSc || (scData !== undefined && scData.status !== 'ok');
  const itFailed = isErrorIt || (itData !== undefined && itData.status !== 'ok');

  const dzAlbums = dzData?.status === 'ok' ? dzData.items : [];
  const scAlbumsRaw = scData?.status === 'ok' ? scData.items : [];
  const itAlbums = itData?.status === 'ok' ? itData.items : [];
  const mergedAlbums = dedupAlbumsByTitle([...dzAlbums, ...scAlbumsRaw, ...itAlbums]);

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

  const isLoadingAlbums = (deezerSource !== null && isLoadingDz)
    || (scSource !== null && isLoadingSc)
    || (itunesSource !== null && isLoadingIt);
  const albumOutcomes = [
    ...(deezerSource !== null ? [dzFailed] : []),
    ...(scSource !== null ? [scFailed] : []),
    ...(itunesSource !== null ? [itFailed] : []),
  ];
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
    refetchAlbums: () => { refetchDz(); refetchSc(); refetchIt(); },
  };
}
