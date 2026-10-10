import { useQuery } from '@tanstack/react-query';

import type { TrackResponse } from '@shared/apiClient/trackResponse';
import { libraryKeys } from '@shared/query/queryKeys';
import type { AlbumGroup } from '@shared/apiClient/library';
import type { DiscoveryResult } from '@shared/apiClient/discovery';

import type { DetailPort } from '@/features/detail/ports/detailPort';
import { useDetailPort } from '@/features/detail/ports/DetailPortProvider';
import { normalizeForCompare } from '@/features/detail/domain/normalizeForCompare';
import { DETAIL_LIST_CAP } from '@/features/detail/domain/contentStatus';

export const LOOKUP_LIMIT = 200;
export const LOOKUP_MAX_PAGES = 5;

type Lookup = { items: TrackResponse[]; complete: boolean };
export type LibraryMatches = TrackResponse[] & { complete?: boolean };

async function fetchLookup(port: DetailPort, q: string, signal?: AbortSignal): Promise<Lookup> {
  const items: TrackResponse[] = [];
  for (let page = 0; page < LOOKUP_MAX_PAGES; page++) {
    const res = await port.getTracks({ limit: LOOKUP_LIMIT, offset: items.length, q }, signal);
    items.push(...res.items);
    if (!res.has_more || res.items.length === 0) return { items, complete: true };
  }
  return { items, complete: false };
}

function useLibraryLookup(search: string): Lookup {
  const port = useDetailPort();
  const trimmed = search.trim();
  const { data: lookup, isError } = useQuery({
    queryKey: libraryKeys.lookup(trimmed),
    queryFn: ({ signal }) => fetchLookup(port, trimmed, signal),
    enabled: trimmed.length > 0,
    staleTime: 60_000,
  });
  return lookup ?? { items: [], complete: !isError };
}

function withCompleteness(matches: TrackResponse[], complete: boolean): LibraryMatches {
  return Object.assign(matches, { complete });
}

export function useLibraryTracksForAlbum(
  albumTitle: string,
  artist: string | null,
): LibraryMatches {
  const { items: candidates, complete } = useLibraryLookup(albumTitle);
  const albumNorm = normalizeForCompare(albumTitle);
  const artistNorm = normalizeForCompare(artist);

  const matches = candidates.filter((t) => {
    const tAlbum = normalizeForCompare(t.album);
    const tArtist = normalizeForCompare(t.album_artist ?? t.artist);
    return tAlbum === albumNorm && (artistNorm === '' || tArtist === artistNorm);
  });
  return withCompleteness(matches, complete);
}

export function useLibraryTracksForArtist(artistName: string): LibraryMatches {
  const { items: candidates, complete } = useLibraryLookup(artistName);
  const artistNorm = normalizeForCompare(artistName);
  const matches = candidates.filter((t) => normalizeForCompare(t.artist) === artistNorm);
  return withCompleteness(matches, complete);
}

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
  const matches: DiscoveryResult[] = [];
  if (!response) return matches;
  for (const group of response.items) {
    if (normalizeForCompare(group.artist) === wanted) matches.push(albumFromLibraryGroup(group));
  }
  return matches;
}
