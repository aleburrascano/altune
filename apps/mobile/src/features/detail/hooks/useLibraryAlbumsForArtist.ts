import { useQuery } from '@tanstack/react-query';

import { getLibraryAlbums, type AlbumGroup } from '@shared/api-client/library';
import type { DiscoveryResult } from '@shared/api-client/discovery';
import { libraryKeys } from '@shared/lib/query-keys';

import { DETAIL_LIST_CAP } from '../content-status';
import { normalizeForCompare } from '../text-compare';

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
  return useQuery({
    queryKey: libraryKeys.albums(artistName, 'recent', DETAIL_LIST_CAP),
    queryFn: ({ signal }) =>
      getLibraryAlbums({ q: artistName, sort: 'recent', limit: DETAIL_LIST_CAP }, signal),
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
