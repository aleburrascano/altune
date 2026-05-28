/**
 * useRecordClick — fire-and-forget POST /v1/discovery/clicks.
 *
 * Slice 45. Failures are swallowed; click tracking is best-effort.
 */

import { useMutation } from '@tanstack/react-query';

import { recordClick, type ClickPayload } from '../../../shared/api-client/discovery';

export function useRecordClick() {
  return useMutation<void, Error, ClickPayload>({
    mutationFn: recordClick,
    onError: () => {
      // Swallow errors; click tracking is best-effort.
    },
  });
}
