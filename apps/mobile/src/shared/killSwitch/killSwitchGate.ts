import { useCallback, useSyncExternalStore } from 'react';

import { isLoopEnabled, onKillSwitchChange, type KillSwitchLoop } from './killSwitch';

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

export function useGatedCallback(loop: KillSwitchLoop, action: () => unknown): () => void {
  const isEnabled = useLoopEnabled(loop);
  return () => {
    if (isEnabled) void action();
  };
}
