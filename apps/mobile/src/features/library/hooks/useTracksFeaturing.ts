import { useQuery } from '@tanstack/react-query';

import { listTracksFeaturing } from '@shared/api-client/tracks';
import type { FeaturedArtist } from '@shared/api-client/types';
import { libraryKeys } from '@shared/lib/query-keys';

const DIGITS = /^[0-9]+$/;

// Route params arrive as strings from deep links, so anything that is not a plain
// non-negative integer (the only shape `String(deezer_id)` produces in-app) is null.
export function parseDeezerIdParam(raw: string | undefined): number | null {
  if (raw === undefined || !DIGITS.test(raw)) return null;
  const id = Number(raw);
  return Number.isSafeInteger(id) ? id : null;
}

export function useTracksFeaturing(input: FeaturedArtist) {
  const nonFiniteId = input.deezer_id != null && !Number.isFinite(input.deezer_id);
  const fa: FeaturedArtist = nonFiniteId ? { ...input, deezer_id: null } : input;
  const key = fa.mbid ?? (fa.deezer_id != null ? `dz:${fa.deezer_id}` : `name:${fa.name}`);
  return useQuery({
    queryKey: libraryKeys.featuring(key),
    queryFn: () => listTracksFeaturing(fa),
    enabled: fa.name.length > 0 || fa.mbid != null || fa.deezer_id != null,
    staleTime: 60_000,
  });
}
