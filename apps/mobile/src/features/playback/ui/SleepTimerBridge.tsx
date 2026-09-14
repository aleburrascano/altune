import { useCallback, useEffect, type ReactElement } from 'react';

import { usePlayback } from '@shared/playback/usePlayback';

import { useAppStateChange } from '../hooks/useAppStateChange';
import { useSleepTimerStore } from '../sleepTimerStore';

export function SleepTimerBridge({
  now = Date.now,
}: {
  now?: () => number;
} = {}): ReactElement | null {
  const endsAt = useSleepTimerStore((s) => s.endsAt);
  const cancel = useSleepTimerStore((s) => s.cancel);
  const { pause } = usePlayback();

  const fire = useCallback((): void => {
    pause();
    cancel();
  }, [pause, cancel]);

  useEffect(() => {
    if (endsAt === null) return;

    const remaining = endsAt - now();
    if (remaining <= 0) {
      fire();
      return;
    }

    const timeout = setTimeout(fire, remaining);
    return () => clearTimeout(timeout);
  }, [endsAt, fire, now]);

  useAppStateChange((next) => {
    if (endsAt !== null && next === 'active' && now() >= endsAt) fire();
  });

  return null;
}
