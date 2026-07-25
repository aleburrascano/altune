import { useQuery } from '@tanstack/react-query';

import { getLibraryAlbums } from '@shared/api-client/library';
import type { DiscoveryResult } from '@shared/api-client/discovery';
import { libraryKeys } from '@shared/lib/query-keys';

const norm = (s: string): string => s.toLowerCase().trim();

export function useLibraryAlbumsForArtist(
  artistName: string,
  enabled: boolean,
): DiscoveryResult[] {
  const { data } = useQuery({
    queryKey: libraryKeys.albums(artistName, 'recent'),
    queryFn: () => getLibraryAlbums({ q: artistName, sort: 'recent' }),
    enabled: enabled && artistName.length > 0,
    staleTime: 60_000,
  });

  const wanted = norm(artistName);
  return (data?.items ?? [])
    .filter((group) => norm(group.artist) === wanted)
    .map((group) => ({
      kind: 'album' as const,
      title: group.album,
      subtitle: group.artist,
      image_url: group.artwork_url,
      confidence: 'high' as const,
      sources: [],
      extras: {
        track_count: group.track_count,
        ...(group.year != null ? { year: group.year } : {}),
      },
    }));
}
