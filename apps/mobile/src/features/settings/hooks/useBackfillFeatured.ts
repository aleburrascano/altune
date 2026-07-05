import { useMutation, useQueryClient } from '@tanstack/react-query';

import { backfillFeaturedArtists } from '@shared/api-client/tracks';

/** Triggers the featured-artist backfill over the user's existing library and
 * refreshes the library list so newly resolved credits appear. */
export function useBackfillFeatured() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: backfillFeaturedArtists,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['library'] });
    },
  });
}
