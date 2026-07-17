/**
 * useRecordEvent — fire-and-forget POST /v1/discovery/events.
 *
 * The unified behavioral-telemetry hook (result_clicked, play, library_add, …),
 * shared across discover / detail / playback. Replaces the legacy per-feature
 * useRecordClick + the /clicks endpoint. Failures are swallowed — telemetry is
 * best-effort and never surfaced to the user (ADR-0007 §3.12).
 */

import { useMutation } from '@tanstack/react-query';

import { recordEvent, type DiscoveryEvent } from './recordEvent';

export function useRecordEvent() {
  return useMutation<void, Error, DiscoveryEvent>({
    mutationFn: recordEvent,
    onError: (error) => {
      console.warn('[discovery] event tracking failed', error);
    },
  });
}
