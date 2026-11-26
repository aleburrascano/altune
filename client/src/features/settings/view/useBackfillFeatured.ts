import { useQueryClient } from '@tanstack/react-query';

import type { BackfillFeaturedResult } from '@shared/apiClient';
import { guardedMutationOptions } from '@shared/session';
import { detailKeys, libraryKeys, useAppMutation } from '@shared/query';
import type { RetryOptions } from '@features/settings/view/retryOptions';
import { ContractError, NetworkError } from '@shared/failure';
import { useSettingsPort } from '@features/settings/view/SettingsPortProvider';
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

function backfillWholeLibrary(port: SettingsPort) {
  return backfillFrom(port, 0, { scanned: 0, updated: 0, truncated: true, nextOffset: 0 });
}

function retryTransientExceptTimeout(
  retryOptions: RetryOptions,
  count: number,
  error: unknown,
): boolean {
  const isTimeout = error instanceof NetworkError && error.failure === 'timeout';
  return !isTimeout && retryOptions.retry(count, error);
}

export function useBackfillFeatured(retryOptions: RetryOptions) {
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
    ...retryOptions,
    retry: (count: number, error: unknown) =>
      retryTransientExceptTimeout(retryOptions, count, error),
  });
}
