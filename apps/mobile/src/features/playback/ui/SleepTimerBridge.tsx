import { useCallback, useEffect, type ReactElement } from 'react';
import type { AppStateStatus } from 'react-native';

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

  const fireIfOverdueOnResume = useCallback(
    (next: AppStateStatus): void => {
      if (endsAt !== null && next === 'active' && now() >= endsAt) fire();
    },
    [endsAt, fire, now],
  );
  useAppStateChange(fireIfOverdueOnResume);

  return null;
}
