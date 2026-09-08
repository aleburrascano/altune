import { useEffect, type ReactElement } from 'react';
import { AppState } from 'react-native';

import { usePlayback } from '@shared/playback/usePlayback';

import { useSleepTimerStore } from '../sleepTimerStore';

export function SleepTimerBridge({
  now = Date.now,
}: {
  now?: () => number;
} = {}): ReactElement | null {
  const endsAt = useSleepTimerStore((s) => s.endsAt);
  const cancel = useSleepTimerStore((s) => s.cancel);
  const { pause } = usePlayback();

  useEffect(() => {
    if (endsAt === null) return;

    const fire = (): void => {
      pause();
      cancel();
    };

    const remaining = endsAt - now();
    if (remaining <= 0) {
      fire();
      return;
    }

    const timeout = setTimeout(fire, remaining);
    const sub = AppState.addEventListener('change', (next) => {
      if (next === 'active' && now() >= endsAt) fire();
    });

    return () => {
      clearTimeout(timeout);
      sub.remove();
    };
  }, [endsAt, pause, cancel, now]);

  return null;
}
