import { useQueryClient } from '@tanstack/react-query';

import type { BackfillFeaturedResult } from '@shared/api-client/tracks';
import { guardedMutationOptions } from '@shared/session/guardedMutation';
import { detailKeys, libraryKeys } from '@shared/query/queryKeys';
import { useAppMutation } from '@shared/query/useAppMutation';
import { ContractError, NetworkError } from '@shared/failure/errors';
import { transientRetryOptions } from '@shared/query/retryDelay';

import { useSettingsPort } from '@features/settings/ports/SettingsPortProvider';
import type { SettingsPort } from '@features/settings/ports/settingsPort';

function nextBackfillOffset(page: BackfillFeaturedResult, offset: number): number {
  if (page.nextOffset > offset) return page.nextOffset;
  throw new ContractError('BackfillFeaturedResult.next_offset', 'did not advance');
}

async function backfillFrom(
  port: SettingsPort,
  offset: number,
  total: BackfillFeaturedResult,
): Promise<BackfillFeaturedResult> {
  const page = await port.backfillFeaturedArtists(offset === 0 ? undefined : offset);
  const scanned = total.scanned + page.scanned;
  const updated = total.updated + page.updated;
  if (!page.truncated) return { ...page, scanned, updated };
  return backfillFrom(port, nextBackfillOffset(page, offset), { ...page, scanned, updated });
}

const backfillWholeLibrary = (port: SettingsPort) =>
  backfillFrom(port, 0, { scanned: 0, updated: 0, truncated: true, nextOffset: 0 });

const retryTransientExceptTimeout: typeof transientRetryOptions.retry = (count, error) =>
  !(error instanceof NetworkError && error.failure === 'timeout') &&
  transientRetryOptions.retry(count, error);

export function useBackfillFeatured() {
  const queryClient = useQueryClient();
  const port = useSettingsPort();
  return useAppMutation({
    action: 'settings.backfill_featured',
    ...guardedMutationOptions({
      mutationFn: () => backfillWholeLibrary(port),
      onSuccess: () => {
        void queryClient.invalidateQueries({ queryKey: libraryKeys.tracksPrefix });
        void queryClient.invalidateQueries({ queryKey: libraryKeys.featuringPrefix });
        void queryClient.invalidateQueries({ queryKey: detailKeys.albumTracksPrefix });
      },
    }),
    ...transientRetryOptions,
    retry: retryTransientExceptTimeout,
  });
}
