import { useCallback, useSyncExternalStore } from 'react';

import { isLoopEnabled, onKillSwitchChange, type KillSwitchLoop } from './killSwitch';

/**
 * Whether `loop` may fetch, re-rendering the caller when the remote switch flips: turning it off
 * stops the work on screens already mounted, turning it back on resumes it without a relaunch.
 */
export function useLoopEnabled(loop: KillSwitchLoop): boolean {
  const subscribe = useCallback(
    (onFlip: () => void) =>
      onKillSwitchChange((flipped) => {
        if (flipped === loop) onFlip();
      }),
    [loop],
  );
  const isEnabled = useCallback(() => isLoopEnabled(loop), [loop]);

  return useSyncExternalStore(subscribe, isEnabled, isEnabled);
}

/**
 * Wraps an affordance that fetches regardless of a query's `enabled` — react-query's `refetch`, a
 * mutation — in the same switch, so a tap cannot reach the endpoint the switch exists to protect.
 */
export function useGatedCallback(loop: KillSwitchLoop, action: () => unknown): () => void {
  const isEnabled = useLoopEnabled(loop);
  return () => {
    if (isEnabled) void action();
  };
}
