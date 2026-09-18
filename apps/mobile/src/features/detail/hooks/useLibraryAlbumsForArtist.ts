import { useQuery } from '@tanstack/react-query';

import { getLibraryAlbums } from '@shared/api-client/library';
import type { DiscoveryResult } from '@shared/api-client/discovery';
import { libraryKeys } from '@shared/lib/query-keys';

import { DETAIL_LIST_CAP } from '../content-status';
import { normalizeForCompare } from '../text-compare';

export function useLibraryAlbumsForArtist(
  artistName: string,
  enabled: boolean,
): DiscoveryResult[] {
  const { data } = useQuery({
    queryKey: libraryKeys.albums(artistName, 'recent', DETAIL_LIST_CAP),
    queryFn: ({ signal }) =>
      getLibraryAlbums({ q: artistName, sort: 'recent', limit: DETAIL_LIST_CAP }, signal),
    enabled: enabled && artistName.length > 0,
    staleTime: 60_000,
  });

  const wanted = normalizeForCompare(artistName);
  return (data?.items ?? [])
    .filter((group) => normalizeForCompare(group.artist) === wanted)
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
