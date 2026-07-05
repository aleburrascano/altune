import { useQuery } from '@tanstack/react-query';

import { listTracksFeaturing } from '@shared/api-client/tracks';
import type { FeaturedArtist } from '@shared/api-client/types';

/** "Everything featuring X" — the user's saved tracks crediting a featured artist.
 * Keyed on the artist's stable identity (mbid, else deezer id, else name). */
export function useTracksFeaturing(fa: FeaturedArtist) {
  const key = fa.mbid ?? (fa.deezer_id != null ? `dz:${fa.deezer_id}` : `name:${fa.name}`);
  return useQuery({
    queryKey: ['library', 'featuring', key],
    queryFn: () => listTracksFeaturing(fa),
    enabled: fa.name.length > 0 || fa.mbid != null || fa.deezer_id != null,
    staleTime: 60_000,
  });
}
