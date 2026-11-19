import { useMutation } from '@tanstack/react-query';

import { recordEvent, type DiscoveryEvent } from '@shared/telemetry/shell/recordEvent';
import { warn } from '@shared/log';

export function useRecordEvent() {
  return useMutation<void, Error, DiscoveryEvent>({
    mutationFn: recordEvent,
    onError: (error) => {
      warn('[discovery] event tracking failed', error);
    },
  });
}
