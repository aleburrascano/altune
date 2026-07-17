import { useMutation, useQueryClient } from '@tanstack/react-query';

import { backfillFeaturedArtists } from '@shared/api-client/tracks';
import { libraryKeys } from '@shared/lib/query-keys';

/** Triggers the featured-artist backfill over the user's existing library and
 * refreshes the library list so newly resolved credits appear. */
export function useBackfillFeatured() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: backfillFeaturedArtists,
    onSuccess: () => {
      // Refresh every cache that renders featured artists: the library snapshot
      // that the tracks list reads, the featuring lists, and album-track lists
      // (so album detail rows pick up the newly resolved credits).
      void queryClient.invalidateQueries({ queryKey: libraryKeys.home });
      void queryClient.invalidateQueries({ queryKey: libraryKeys.featuringPrefix });
      void queryClient.invalidateQueries({ queryKey: ['album-tracks'] });
    },
  });
}
