import { useMutation, useQueryClient } from '@tanstack/react-query';

import { isRetryable } from '@shared/api-client';
import { backfillFeaturedArtists } from '@shared/api-client/tracks';
import { currentSessionEpoch, isSameSession } from '@shared/session/signOutCleanup';
import { libraryKeys } from '@shared/lib/query-keys';

export function useBackfillFeatured() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: backfillFeaturedArtists,
    // Mutations get no retry by default; a re-run backfill is harmless, so retry
    // transient failures with the same policy queries use (_layout.tsx).
    retry: (failureCount, error) => isRetryable(error) && failureCount < 5,
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
