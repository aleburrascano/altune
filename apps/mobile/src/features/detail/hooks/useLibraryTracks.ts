import { useQuery } from '@tanstack/react-query';

import { getTracks } from '@shared/api-client/tracks';
import type { TrackResponse } from '@shared/api-client/types';
import { libraryKeys } from '@shared/lib/query-keys';

const LOOKUP_LIMIT = 200;

const norm = (s: string | null): string => (s ?? '').toLowerCase().trim();

function useLibraryLookup(search: string): TrackResponse[] {
  const trimmed = search.trim();
  const { data } = useQuery({
    queryKey: libraryKeys.lookup(trimmed),
    queryFn: () => getTracks({ limit: LOOKUP_LIMIT, offset: 0, q: trimmed }),
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
  const albumNorm = norm(albumTitle);
  const artistNorm = norm(artist);

  return candidates.filter((t) => {
    const tAlbum = norm(t.album);
    const tArtist = norm(t.album_artist ?? t.artist);
    return tAlbum === albumNorm && (artistNorm === '' || tArtist === artistNorm);
  });
}

export function useLibraryTracksForArtist(artistName: string): TrackResponse[] {
  const candidates = useLibraryLookup(artistName);
  const artistNorm = norm(artistName);
  return candidates.filter((t) => norm(t.artist) === artistNorm);
}
