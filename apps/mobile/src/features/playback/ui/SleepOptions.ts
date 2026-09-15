import { useEffect, useState } from 'react';

import type { ActionSheetOption } from '@shared/ui/primitives/ActionSheet';

import { minutesRemaining, useSleepTimerStore } from '../sleepTimerStore';

const SLEEP_OPTIONS_MIN = [15, 30, 45, 60];

export type SleepOptions = {
  /** Timer state, shown on the root menu row. */
  valueLabel: string;
  subtitle: string | undefined;
  options: ActionSheetOption[];
};

// Read the wall clock outside render (react-hooks/purity): capture it in state
// and refresh while a timer is running — immediately via rAF on change, then
// once a second to keep the countdown live.
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
