/**
 * SleepTimerBridge — pauses playback when the sleep timer's deadline arrives.
 *
 * Mounted once inside the PlaybackProvider (it needs `pause`), renders nothing.
 * The timer must keep working while the player screen is closed, so the effect
 * cannot live in FullPlayer.
 *
 * Backgrounding is the hard case: a `setTimeout` scheduled for 30 minutes out is
 * not guaranteed to fire on time once the OS suspends the JS thread. So the
 * deadline is re-checked on every foreground transition, and an already-elapsed
 * deadline pauses immediately.
 */
import { useEffect, type ReactElement } from 'react';
import { AppState } from 'react-native';

import { usePlayback } from '@shared/playback/usePlayback';

import { useSleepTimerStore } from '../sleepTimerStore';

export function SleepTimerBridge(): ReactElement | null {
  const endsAt = useSleepTimerStore((s) => s.endsAt);
  const cancel = useSleepTimerStore((s) => s.cancel);
  const { pause } = usePlayback();

  useEffect(() => {
    if (endsAt === null) return;

    const fire = (): void => {
      pause();
      cancel();
    };

    const remaining = endsAt - Date.now();
    if (remaining <= 0) {
      fire();
      return;
    }

    const timeout = setTimeout(fire, remaining);
    const sub = AppState.addEventListener('change', (next) => {
      if (next === 'active' && Date.now() >= endsAt) fire();
    });

    return () => {
      clearTimeout(timeout);
      sub.remove();
    };
  }, [endsAt, pause, cancel]);

  return null;
}
