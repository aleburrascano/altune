/**
 * Announce a message whenever it changes — the iOS half of a live region.
 *
 * Skips the first value: mounting a surface that already says "Downloading 3
 * songs" is not a change worth interrupting the user for. Only transitions are.
 */
import { useEffect, useRef } from 'react';

import { announce } from './announce';

export function useAnnounceChange(message: string): void {
  const previous = useRef<string | null>(null);
  useEffect(() => {
    if (previous.current !== null && previous.current !== message) {
      announce(message);
    }
    previous.current = message;
  }, [message]);
}
