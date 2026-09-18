import { useQuery } from '@tanstack/react-query';

import { getTracks } from '@shared/api-client/tracks';
import type { TrackResponse } from '@shared/api-client/types';
import { libraryKeys } from '@shared/lib/query-keys';

import { normalizeForCompare } from '../text-compare';

const LOOKUP_LIMIT = 200;

function useLibraryLookup(search: string): TrackResponse[] {
  const trimmed = search.trim();
  const { data } = useQuery({
    queryKey: libraryKeys.lookup(trimmed),
    queryFn: ({ signal }) => getTracks({ limit: LOOKUP_LIMIT, offset: 0, q: trimmed }, signal),
    enabled: trimmed.length > 0,
    staleTime: 60_000,
  });
  return data?.items ?? [];
}

export function useLibraryTracksForAlbum(
  albumTitle: string,
  artist: string | null,
): TrackResponse[] {
  const candidates = useLibraryLookup(albumTitle);
  const albumNorm = normalizeForCompare(albumTitle);
  const artistNorm = normalizeForCompare(artist);

  return candidates.filter((t) => {
    const tAlbum = normalizeForCompare(t.album);
    const tArtist = normalizeForCompare(t.album_artist ?? t.artist);
    return tAlbum === albumNorm && (artistNorm === '' || tArtist === artistNorm);
  });
}

export function useLibraryTracksForArtist(artistName: string): TrackResponse[] {
  const candidates = useLibraryLookup(artistName);
  const artistNorm = normalizeForCompare(artistName);
  return candidates.filter((t) => normalizeForCompare(t.artist) === artistNorm);
}
