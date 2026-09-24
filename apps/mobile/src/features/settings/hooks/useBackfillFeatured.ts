import { useMutation, useQueryClient } from '@tanstack/react-query';

import { isRetryable } from '@shared/api-client';
import { backfillFeaturedArtists } from '@shared/api-client/tracks';
import { guardedMutationOptions } from '@shared/session/signOutCleanup';
import { detailKeys, libraryKeys } from '@shared/lib/query-keys';
import { retryDelayMs } from '@shared/query/retryDelay';

export function useBackfillFeatured() {
  const queryClient = useQueryClient();
  return useMutation({
    ...guardedMutationOptions({
      mutationFn: backfillFeaturedArtists,
      onSuccess: () => {
        void queryClient.invalidateQueries({ queryKey: libraryKeys.tracksPrefix });
        void queryClient.invalidateQueries({ queryKey: libraryKeys.featuringPrefix });
        void queryClient.invalidateQueries({ queryKey: detailKeys.albumTracksPrefix });
      },
    }),
    // Mutations get no retry by default; a re-run backfill is harmless, so retry
    // transient failures on the same policy and jittered backoff queries use
    // (_layout.tsx). Un-jittered, every client failing on one outage retries together.
    retry: (failureCount, error) => isRetryable(error) && failureCount < 5,
    retryDelay: (failureCount) => retryDelayMs(failureCount, Math.random()),
  });
}
