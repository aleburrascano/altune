import { useMutation } from '@tanstack/react-query';

import { isTelemetryGated } from '@shared/errors';

import { recordEvent, type DiscoveryEvent } from './recordEvent';

export function useRecordEvent() {
  return useMutation<void, Error, DiscoveryEvent>({
    mutationFn: recordEvent,
    onError: (error) => {
      if (isTelemetryGated(error)) return;
      console.warn('[discovery] event tracking failed', error);
    },
  });
}
