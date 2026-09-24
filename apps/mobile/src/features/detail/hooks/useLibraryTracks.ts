import { useQuery } from '@tanstack/react-query';

import { getTracks } from '@shared/api-client/tracks';
import type { TrackResponse } from '@shared/api-client/types';
import { libraryKeys } from '@shared/lib/query-keys';

import { normalizeForCompare } from '../text-compare';

export const LOOKUP_LIMIT = 200;
export const LOOKUP_MAX_PAGES = 5;

type Lookup = { items: TrackResponse[]; complete: boolean };
export type LibraryMatches = TrackResponse[] & { complete?: boolean };

async function fetchLookup(q: string, signal?: AbortSignal): Promise<Lookup> {
  const items: TrackResponse[] = [];
  for (let page = 0; page < LOOKUP_MAX_PAGES; page++) {
    const res = await getTracks({ limit: LOOKUP_LIMIT, offset: items.length, q }, signal);
    items.push(...res.items);
    if (!res.has_more || res.items.length === 0) return { items, complete: true };
  }
  return { items, complete: false };
}

function useLibraryLookup(search: string): Lookup {
  const trimmed = search.trim();
  const { data: lookup } = useQuery({
    queryKey: libraryKeys.lookup(trimmed),
    queryFn: ({ signal }) => fetchLookup(trimmed, signal),
    enabled: trimmed.length > 0,
    staleTime: 60_000,
  });
  return lookup ?? { items: [], complete: true };
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

export function useLibraryTracksForArtist(artistName: string): TrackResponse[] {
  const { items: candidates } = useLibraryLookup(artistName);
  const artistNorm = normalizeForCompare(artistName);
  return candidates.filter((t) => normalizeForCompare(t.artist) === artistNorm);
}
