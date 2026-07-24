import { useRef, useState, type ReactElement } from 'react';

import { usePlayback } from '@shared/playback/usePlayback';
import { ActionSheet, type ActionSheetOption } from '@shared/ui/primitives/ActionSheet';

import { PLAYBACK_RATES, rateLabel, usePlaybackRateStore } from '../playbackRateStore';
import { minutesRemaining, useSleepTimerStore } from '../sleepTimerStore';

const SLEEP_OPTIONS_MIN = [15, 30, 45, 60];

type OpenSheet = 'root' | 'speed' | 'sleep' | null;

export function PlayerOptionsSheets({
  open,
  onClose,
}: {
  open: boolean;
  onClose: () => void;
}): ReactElement {
  const [sheet, setSheet] = useState<OpenSheet>(null);
  const { setRate: applyRate } = usePlayback();
  const rate = usePlaybackRateStore((s) => s.rate);
  const setStoredRate = usePlaybackRateStore((s) => s.setRate);
  const endsAt = useSleepTimerStore((s) => s.endsAt);
  const startSleep = useSleepTimerStore((s) => s.start);
  const cancelSleep = useSleepTimerStore((s) => s.cancel);

  const visible: OpenSheet = sheet ?? (open ? 'root' : null);

  const close = (): void => {
    setSheet(null);
    onClose();
  };

  const nextSheet = useRef<OpenSheet>(null);
  const closeRoot = (): void => {
    const next = nextSheet.current;
    nextSheet.current = null;
    if (next !== null) {
      setSheet(next);
      return;
    }
    close();
  };

  const remaining = minutesRemaining(endsAt, Date.now());
  const sleepValue = endsAt === null ? 'Off' : `${remaining} min left`;

  const rootOptions: ActionSheetOption[] = [
    {
      label: `Playback speed · ${rateLabel(rate)}`,
      testID: 'player-options-speed',
      onPress: () => {
        nextSheet.current = 'speed';
      },
    },
    {
      label: `Sleep timer · ${sleepValue}`,
      testID: 'player-options-sleep',
      onPress: () => {
        nextSheet.current = 'sleep';
      },
    },
  ];

  const speedOptions: ActionSheetOption[] = PLAYBACK_RATES.map((r) => ({
    label: r === rate ? `${rateLabel(r)}  ✓` : rateLabel(r),
    testID: `player-rate-${r}`,
    onPress: () => {
      setStoredRate(r);
      applyRate(r);
    },
  }));

  const sleepOptions: ActionSheetOption[] = [
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

  return (
    <>
      <ActionSheet
        testID="player-options-sheet"
        visible={visible === 'root'}
        title="Player options"
        options={rootOptions}
        onClose={closeRoot}
      />
      <ActionSheet
        testID="player-speed-sheet"
        visible={visible === 'speed'}
        title="Playback speed"
        options={speedOptions}
        onClose={close}
      />
      <ActionSheet
        testID="player-sleep-sheet"
        visible={visible === 'sleep'}
        title="Sleep timer"
        subtitle={endsAt === null ? undefined : `Pausing in ${remaining} min`}
        options={sleepOptions}
        onClose={close}
      />
    </>
  );
}
