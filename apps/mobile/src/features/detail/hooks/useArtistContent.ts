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

  const tracksQuery = useQuery({
    queryKey: ['artist-top-tracks', streamSource?.provider ?? '', streamSource?.external_id ?? ''],
    queryFn: () => getArtistTopTracks(streamSource!.provider, streamSource!.external_id, 5),
    enabled: enabled && streamSource !== null,
    staleTime: 1000 * 60 * 30,
  });

  const mbAlbumsQuery = useQuery({
    queryKey: ['artist-albums-mb', mbSource?.external_id ?? ''],
    queryFn: () => getArtistAlbums('musicbrainz', mbSource!.external_id, 100),
    enabled: enabled && mbSource !== null,
    staleTime: 1000 * 60 * 30,
  });

  const deezerAlbumsQuery = useQuery({
    queryKey: ['artist-albums-dz', deezerSource?.external_id ?? ''],
    queryFn: () => getArtistAlbums('deezer', deezerSource!.external_id, 100),
    enabled: enabled && deezerSource !== null,
    staleTime: 1000 * 60 * 30,
  });

  const mbAlbums = mbAlbumsQuery.data?.items ?? [];
  const dzAlbums = deezerAlbumsQuery.data?.items ?? [];
  const mergedAlbums = dedupAlbumsByTitle([...mbAlbums, ...dzAlbums]);

  const isLoadingAlbums = (mbSource !== null && mbAlbumsQuery.isLoading)
    || (deezerSource !== null && deezerAlbumsQuery.isLoading);
  const isErrorAlbums = (mbSource !== null && mbAlbumsQuery.isError)
    && (deezerSource === null || deezerAlbumsQuery.isError);

  return {
    topTracks: tracksQuery.data?.items ?? [],
    albums: sortByReleaseDateDesc(mergedAlbums),
    isLoadingTracks: tracksQuery.isLoading,
    isLoadingAlbums,
    isErrorTracks: tracksQuery.isError || tracksQuery.data?.status === 'error',
    isErrorAlbums,
    refetchTracks: tracksQuery.refetch,
    refetchAlbums: () => { mbAlbumsQuery.refetch(); deezerAlbumsQuery.refetch(); },
  };
}
