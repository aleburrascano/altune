import { useQueryClient } from '@tanstack/react-query';

import { backfillFeaturedArtists } from '@shared/api-client/tracks';
import { guardedMutationOptions } from '@shared/session/signOutCleanup';
import { detailKeys, libraryKeys } from '@shared/lib/query-keys';
import { useAppMutation } from '@shared/query/useAppMutation';
import { transientRetryOptions } from '@shared/query/retryDelay';

export function useBackfillFeatured() {
  const queryClient = useQueryClient();
  return useAppMutation({
    action: 'settings.backfill_featured',
    ...guardedMutationOptions({
      mutationFn: backfillFeaturedArtists,
      onSuccess: () => {
        void queryClient.invalidateQueries({ queryKey: libraryKeys.tracksPrefix });
        void queryClient.invalidateQueries({ queryKey: libraryKeys.featuringPrefix });
        void queryClient.invalidateQueries({ queryKey: detailKeys.albumTracksPrefix });
      },
    }),
    ...transientRetryOptions,
  });
}
