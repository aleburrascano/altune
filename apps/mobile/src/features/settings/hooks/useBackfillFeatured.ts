import { useMutation, useQueryClient } from '@tanstack/react-query';

import { backfillFeaturedArtists } from '@shared/api-client/tracks';
import { libraryKeys } from '@shared/lib/query-keys';

export function useBackfillFeatured() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: backfillFeaturedArtists,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: libraryKeys.tracksPrefix });
      void queryClient.invalidateQueries({ queryKey: libraryKeys.featuringPrefix });
      void queryClient.invalidateQueries({ queryKey: ['album-tracks'] });
    },
  });
}
