import { useMutation, useQueryClient } from '@tanstack/react-query';

import { backfillFeaturedArtists } from '@shared/api-client/tracks';

/** Triggers the featured-artist backfill over the user's existing library and
 * refreshes the library list so newly resolved credits appear. */
export function useBackfillFeatured() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: backfillFeaturedArtists,
    onSuccess: () => {
      // Refresh every cache that renders featured artists: the library snapshot
      // that the songs list reads, the infinite library cache, and album-track
      // lists (so album detail rows pick up the newly resolved credits).
      void queryClient.invalidateQueries({ queryKey: ['library-home'] });
      void queryClient.invalidateQueries({ queryKey: ['library'] });
      void queryClient.invalidateQueries({ queryKey: ['album-tracks'] });
    },
  });
}
