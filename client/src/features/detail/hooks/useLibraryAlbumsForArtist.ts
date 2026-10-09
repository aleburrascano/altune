import { useQuery } from '@tanstack/react-query';

import type { AlbumGroup } from '@shared/api-client/library';
import type { DiscoveryResult } from '@shared/api-client/discovery';
import { libraryKeys } from '@shared/query/queryKeys';

import { DETAIL_LIST_CAP } from '@/features/detail/domain/content-status';
import { useDetailPort } from '@/features/detail/ports/DetailPortProvider';
import { normalizeForCompare } from '@/features/detail/domain/normalizeForCompare';

function albumExtrasFor(group: AlbumGroup) {
  return { track_count: group.track_count, ...(group.year != null ? { year: group.year } : {}) };
}

function albumIdentity(group: AlbumGroup) {
  return { title: group.album, subtitle: group.artist, image_url: group.artwork_url };
}

function albumFromLibraryGroup(group: AlbumGroup): DiscoveryResult {
  return {
    kind: 'album',
    ...albumIdentity(group),
    confidence: 'high',
    sources: [],
    extras: albumExtrasFor(group),
  };
}

function useLibraryAlbumsQuery(artistName: string, enabled: boolean) {
  const port = useDetailPort();
  return useQuery({
    queryKey: libraryKeys.albums(artistName, 'recent', DETAIL_LIST_CAP),
    queryFn: ({ signal }) =>
      port.getLibraryAlbums({ q: artistName, sort: 'recent', limit: DETAIL_LIST_CAP }, signal),
    enabled: enabled && artistName.length > 0,
    staleTime: 60_000,
  });
}

export function useLibraryAlbumsForArtist(artistName: string, enabled: boolean): DiscoveryResult[] {
  const { data: response } = useLibraryAlbumsQuery(artistName, enabled);
  const wanted = normalizeForCompare(artistName);
  return (response?.items ?? [])
    .filter((group) => normalizeForCompare(group.artist) === wanted)
    .map(albumFromLibraryGroup);
}
