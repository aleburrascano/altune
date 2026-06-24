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
    onError: (error) => {
      // Best-effort telemetry (ADR-0007 §3.12): never surfaced to the user,
      // but logged for observability rather than silently swallowed.
      console.warn('[discovery] click tracking failed', error);
    },
  });
}
