import { useMutation, useQueryClient } from '@tanstack/react-query';

import { backfillFeaturedArtists } from '@shared/api-client/tracks';
import { currentSessionEpoch, isSameSession } from '@shared/auth/signOutCleanup';
import { libraryKeys } from '@shared/lib/query-keys';

export function useBackfillFeatured() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: backfillFeaturedArtists,
    onMutate: () => ({ epoch: currentSessionEpoch() }),
    onSuccess: (_result, _vars, context) => {
      // A backfill that settles after sign-out must not touch the next user's cache.
      if (!isSameSession(context?.epoch)) return;
      void queryClient.invalidateQueries({ queryKey: libraryKeys.tracksPrefix });
      void queryClient.invalidateQueries({ queryKey: libraryKeys.featuringPrefix });
      void queryClient.invalidateQueries({ queryKey: ['album-tracks'] });
    },
  });
}
