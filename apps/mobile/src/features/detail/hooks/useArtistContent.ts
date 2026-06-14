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
import { normalizeForDedup } from '@shared/lib/normalize-for-dedup';



function getReleaseSortKey(album: DiscoveryResult): string | null {
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

function _mergedSources(a: DiscoverySource[], b: DiscoverySource[]): DiscoverySource[] {
  const seen = new Set(a.map((s) => `${s.provider}:${s.external_id}`));
  const merged = [...a];
  for (const s of b) {
    const key = `${s.provider}:${s.external_id}`;
    if (!seen.has(key)) {
      seen.add(key);
      merged.push(s);
    }
  }
  return merged;
}

function dedupAlbumsByTitle(albums: DiscoveryResult[]): DiscoveryResult[] {
  const groups = new Map<string, DiscoveryResult>();
  for (const album of albums) {
    const key = normalizeForDedup(album.title);
    const existing = groups.get(key);
    if (existing === undefined) {
      groups.set(key, album);
    } else {
      const existingCount = typeof existing.extras['track_count'] === 'number' ? existing.extras['track_count'] : 0;
      const newCount = typeof album.extras['track_count'] === 'number' ? album.extras['track_count'] : 0;
      const merged = _mergedSources(existing.sources, album.sources);
      if (newCount > existingCount) {
        groups.set(key, { ...album, sources: merged });
      } else {
        groups.set(key, { ...existing, sources: merged });
      }
    }
  }
  return Array.from(groups.values());
}

type UseArtistContentParams = {
  sources: DiscoverySource[];
  /** Authoritative artist MBID from extras.mbid — picks the right MB source
   *  when the merged card carries several same-name MusicBrainz artists. */
  mbid?: string | null;
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
  mbid = null,
  enabled = true,
}: UseArtistContentParams): UseArtistContentReturn {
  const mbSource =
    (mbid !== null
      ? sources.find((s) => s.provider === 'musicbrainz' && s.external_id === mbid)
      : undefined)
    ?? sources.find((s) => s.provider === 'musicbrainz')
    ?? null;
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

  const {
    data: mbData,
    isLoading: isLoadingMb,
    isError: isErrorMb,
    refetch: refetchMb,
  } = useQuery({
    queryKey: ['artist-albums-mb', mbSource?.external_id ?? ''],
    queryFn: () => getArtistAlbums('musicbrainz', mbSource!.external_id, 100),
    enabled: enabled && mbSource !== null,
    staleTime: 1000 * 60 * 30,
  });

  const {
    data: dzData,
    isLoading: isLoadingDz,
    isError: isErrorDz,
    refetch: refetchDz,
  } = useQuery({
    queryKey: ['artist-albums-dz', deezerSource?.external_id ?? ''],
    queryFn: () => getArtistAlbums('deezer', deezerSource!.external_id, 100),
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

  const mbFailed = isErrorMb || (mbData !== undefined && mbData.status !== 'ok');
  const dzFailed = isErrorDz || (dzData !== undefined && dzData.status !== 'ok');
  const scFailed = isErrorSc || (scData !== undefined && scData.status !== 'ok');

  const mbAlbums = mbData?.status === 'ok' ? mbData.items : [];
  const dzAlbumsRaw = dzData?.status === 'ok' ? dzData.items : [];
  const scAlbumsRaw = scData?.status === 'ok' ? scData.items : [];
  // MB-authoritative discography: Deezer's artist entities can conflate
  // several same-name artists (and its album entries carry no artist field
  // to filter on). With a VERIFIED identity (mbid matches the MB source we
  // queried) and a healthy MB list, Deezer only enriches title-matched
  // albums — it contributes no new titles. Without that, the union stands.
  const mbAuthoritative =
    mbid !== null && mbSource?.external_id === mbid && mbAlbums.length > 0;
  const mbTitleKeys = new Set(mbAlbums.map((a) => normalizeForDedup(a.title)));
  const dzAlbums = mbAuthoritative
    ? dzAlbumsRaw.filter((a) => mbTitleKeys.has(normalizeForDedup(a.title)))
    : dzAlbumsRaw;
  const mergedAlbums = dedupAlbumsByTitle([...mbAlbums, ...dzAlbums, ...scAlbumsRaw]);

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

  const isLoadingAlbums = (mbSource !== null && isLoadingMb)
    || (deezerSource !== null && isLoadingDz)
    || (scSource !== null && isLoadingSc);
  const albumOutcomes = [
    ...(mbSource !== null ? [mbFailed] : []),
    ...(deezerSource !== null ? [dzFailed] : []),
    ...(scSource !== null ? [scFailed] : []),
  ];
  const isErrorAlbums = albumOutcomes.length > 0 && albumOutcomes.every(Boolean);

  return {
    topTracks: tracksData?.status === 'ok' ? tracksData.items : [],
    albums: sortByReleaseDateDesc(albumsWithArt),
    isLoadingTracks: isLoadingTracksRaw,
    isLoadingAlbums,
    isErrorTracks:
      isErrorTracksRaw || (tracksData !== undefined && tracksData.status !== 'ok'),
    isErrorAlbums,
    refetchTracks: refetchTracksRaw,
    refetchAlbums: () => { refetchMb(); refetchDz(); refetchSc(); },
  };
}
