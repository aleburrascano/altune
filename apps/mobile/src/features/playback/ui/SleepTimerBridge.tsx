import { useCallback, useEffect, type ReactElement } from 'react';

import { usePlayback } from '@shared/playback/usePlayback';

import { useAppStateChange } from '@shared/lifecycle';
import { monotonicNow as defaultMonotonicNow, useSleepTimerStore } from '../sleepTimerStore';

const MAX_SLEEP_CHECK_MS = 60_000;

export function SleepTimerBridge({
  monotonicNow = defaultMonotonicNow,
}: {
  monotonicNow?: () => number;
} = {}): ReactElement | null {
  const monoDeadline = useSleepTimerStore((s) => s.monoDeadline);
  const cancel = useSleepTimerStore((s) => s.cancel);
  const { pause } = usePlayback();

  const fire = useCallback((): void => {
    pause();
    cancel();
  }, [pause, cancel]);

  useEffect(() => {
    if (monoDeadline === null) return;

    let timeout: ReturnType<typeof setTimeout> | undefined;
    const check = (): void => {
      const remaining = monoDeadline - monotonicNow();
      if (remaining <= 0) {
        fire();
        return;
      }
      timeout = setTimeout(check, Math.min(remaining, MAX_SLEEP_CHECK_MS));
    };
    check();
    return () => clearTimeout(timeout);
  }, [monoDeadline, fire, monotonicNow]);

  useAppStateChange((next) => {
    if (monoDeadline !== null && next === 'active' && monotonicNow() >= monoDeadline) fire();
  });

  return null;
}
