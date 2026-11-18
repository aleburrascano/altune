import { useMutation } from '@tanstack/react-query';

import { isTelemetryGated } from '@shared/failure';

import { recordEvent, type DiscoveryEvent } from '@shared/telemetry/recordEvent';
import { warn } from '@shared/log';

export function useRecordEvent() {
  return useMutation<void, Error, DiscoveryEvent>({
    mutationFn: recordEvent,
    onError: (error) => {
      if (isTelemetryGated(error)) return;
      warn('[discovery] event tracking failed', error);
    },
  });
}
