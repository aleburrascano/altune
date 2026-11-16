import { useEffect, useRef } from 'react';

import { announce } from '@shared/ui/a11y/announce';

export function useAnnounceChange(message: string): void {
  const previous = useRef<string | null>(null);
  useEffect(() => {
    if (previous.current !== null && previous.current !== message) {
      announce(message);
    }
    previous.current = message;
  }, [message]);
}
