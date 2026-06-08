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

const CONTENT_PROVIDERS = ['deezer', 'lastfm', 'musicbrainz'] as const;

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

function normalizeForDedup(title: string): string {
  return title
    .replace(/[\(\[\{][^\)\]\}]*[\)\]\}]/g, ' ')
    .toLowerCase()
    .replace(/\s+/g, ' ')
    .trim();
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
      if (newCount > existingCount) {
        groups.set(key, album);
      }
    }
  }
  return Array.from(groups.values());
}

function bestSourcePerProvider(
  sources: DiscoverySource[],
  providers: readonly string[],
): DiscoverySource[] {
  const result: DiscoverySource[] = [];
  const seen = new Set<string>();
  for (const p of providers) {
    if (seen.has(p)) continue;
    const match = sources.find((s) => s.provider === p);
    if (match) {
      seen.add(p);
      result.push(match);
    }
  }
  return result;
}

type UseArtistContentParams = {
  sources: DiscoverySource[];
  artistName: string;
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
  const contentSources = bestSourcePerProvider(sources, CONTENT_PROVIDERS);
  const trackSource = contentSources[0] ?? null;
  const scName = artistName.trim();

  const tracksQuery = useQuery({
    queryKey: ['artist-top-tracks', trackSource?.provider ?? '', trackSource?.external_id ?? ''],
    queryFn: () => getArtistTopTracks(trackSource!.provider, trackSource!.external_id, 5),
    enabled: enabled && trackSource !== null,
    staleTime: 1000 * 60 * 30,
  });

  const albumsQuery = useQuery({
    queryKey: ['artist-albums-multi', ...contentSources.map((s) => `${s.provider}:${s.external_id}`)],
    queryFn: async () => {
      const results = await Promise.allSettled(
        contentSources.map((s: DiscoverySource) => getArtistAlbums(s.provider, s.external_id, 100)),
      );
      const allAlbums = results
        .filter(
          (r): r is PromiseFulfilledResult<Awaited<ReturnType<typeof getArtistAlbums>>> =>
            r.status === 'fulfilled',
        )
        .flatMap((r: PromiseFulfilledResult<Awaited<ReturnType<typeof getArtistAlbums>>>) => r.value.items);
      return dedupAlbumsByTitle(allAlbums);
    },
    enabled: enabled && contentSources.length > 0,
    staleTime: 1000 * 60 * 30,
  });

  const scAlbumsQuery = useQuery({
    queryKey: ['artist-albums-sc', scName],
    queryFn: () => getArtistAlbums('soundcloud', scName, 30),
    enabled: enabled && scName.length > 0,
    staleTime: 1000 * 60 * 30,
  });

  const otherProviderAlbums = albumsQuery.data ?? [];
  const scAlbums = scAlbumsQuery.data?.items ?? [];
  const allRaw = [...otherProviderAlbums, ...scAlbums];

  const mergedAlbums = dedupAlbumsByTitle(allRaw).map((album) => {
    if (album.image_url) return album;
    const key = normalizeForDedup(album.title);
    const donor = allRaw.find((a) => normalizeForDedup(a.title) === key && a.image_url);
    if (!donor) return album;
    return { ...album, image_url: donor.image_url };
  });

  return {
    topTracks: tracksQuery.data?.items ?? [],
    albums: sortByReleaseDateDesc(mergedAlbums),
    isLoadingTracks: tracksQuery.isLoading,
    isLoadingAlbums: albumsQuery.isLoading && scAlbumsQuery.isLoading,
    isErrorTracks: tracksQuery.isError || tracksQuery.data?.status === 'error',
    isErrorAlbums: albumsQuery.isError && scAlbumsQuery.isError,
    refetchTracks: tracksQuery.refetch,
    refetchAlbums: () => { albumsQuery.refetch(); scAlbumsQuery.refetch(); },
  };
}
