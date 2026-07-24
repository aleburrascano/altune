import { useQuery } from '@tanstack/react-query';

import { listTracksFeaturing } from '@shared/api-client/tracks';
import type { FeaturedArtist } from '@shared/api-client/types';
import { libraryKeys } from '@shared/lib/query-keys';

export function useTracksFeaturing(fa: FeaturedArtist) {
  const key = fa.mbid ?? (fa.deezer_id != null ? `dz:${fa.deezer_id}` : `name:${fa.name}`);
  return useQuery({
    queryKey: libraryKeys.featuring(key),
    queryFn: () => listTracksFeaturing(fa),
    enabled: fa.name.length > 0 || fa.mbid != null || fa.deezer_id != null,
    staleTime: 60_000,
  });
}
