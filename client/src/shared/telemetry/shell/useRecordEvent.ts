import { useMutation } from '@tanstack/react-query';

import { recordEvent } from '@shared/telemetry/shell/currentTelemetry';
import { warn } from '@shared/log';

export function useRecordEvent() {
  return useMutation({
    mutationFn: recordEvent,
    onError: (error) => {
      warn('[discovery] event tracking failed', error);
    },
  });
}
