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
