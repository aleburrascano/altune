import { useEffect, useState } from 'react';
import { create } from 'zustand';

import type { ActionSheetOption } from '@shared/ui/primitives/ActionSheet';
import { monotonicNow } from '@shared/time';

type SleepTimerState = {
  endsAt: number | null;
  monoDeadline: number | null;
  minutes: number | null;
  start: (minutes: number, now?: number, mono?: number) => void;
  cancel: () => void;
};

export const useSleepTimerStore = create<SleepTimerState>((set) => ({
  endsAt: null,
  monoDeadline: null,
  minutes: null,
  start: (minutes, now = Date.now(), mono = monotonicNow()) =>
    set({ endsAt: now + minutes * 60_000, monoDeadline: mono + minutes * 60_000, minutes }),
  cancel: () => set({ endsAt: null, monoDeadline: null, minutes: null }),
}));

function minutesRemaining(endsAt: number | null, now: number): number {
  if (endsAt === null) return 0;
  return Math.max(0, Math.ceil((endsAt - now) / 60_000));
}

const SLEEP_OPTIONS_MIN = [15, 30, 45, 60];

export type SleepOptions = {
  valueLabel: string;
  subtitle: string | undefined;
  options: ActionSheetOption[];
};

function useNow(endsAt: number | null): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (endsAt === null) return undefined;
    const tick = (): void => setNow(Date.now());
    const raf = requestAnimationFrame(tick);
    const id = setInterval(tick, 1000);
    return () => {
      cancelAnimationFrame(raf);
      clearInterval(id);
    };
  }, [endsAt]);
  return now;
}

export function useSleepOptions(): SleepOptions {
  const endsAt = useSleepTimerStore((s) => s.endsAt);
  const startSleep = useSleepTimerStore((s) => s.start);
  const cancelSleep = useSleepTimerStore((s) => s.cancel);
  const now = useNow(endsAt);

  const remaining = minutesRemaining(endsAt, now);

  const options: ActionSheetOption[] = [
    ...SLEEP_OPTIONS_MIN.map((m) => ({
      label: `${m} minutes`,
      testID: `player-sleep-${m}`,
      onPress: () => startSleep(m),
    })),
    ...(endsAt !== null
      ? [
          {
            label: 'Turn off sleep timer',
            tone: 'danger' as const,
            testID: 'player-sleep-off',
            onPress: cancelSleep,
          },
        ]
      : []),
  ];

  if (endsAt === null) return { valueLabel: 'Off', subtitle: undefined, options };
  return {
    valueLabel: `${remaining} min left`,
    subtitle: `Pausing in ${remaining} min`,
    options,
  };
}
