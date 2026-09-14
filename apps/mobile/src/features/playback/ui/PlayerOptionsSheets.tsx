import { useRef, useState, type ReactElement } from 'react';

import { ActionSheet, type ActionSheetOption } from '@shared/ui/primitives/ActionSheet';

import { useSleepOptions } from './SleepOptions';
import { useSpeedOptions } from './SpeedOptions';

type OpenSheet = 'root' | 'speed' | 'sleep' | null;

export function PlayerOptionsSheets({
  open,
  onClose,
}: {
  open: boolean;
  onClose: () => void;
}): ReactElement {
  const [sheet, setSheet] = useState<OpenSheet>(null);
  const speed = useSpeedOptions();
  const sleep = useSleepOptions();

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

  const rootOptions: ActionSheetOption[] = [
    {
      label: `Playback speed · ${speed.valueLabel}`,
      testID: 'player-options-speed',
      onPress: () => {
        nextSheet.current = 'speed';
      },
    },
    {
      label: `Sleep timer · ${sleep.valueLabel}`,
      testID: 'player-options-sleep',
      onPress: () => {
        nextSheet.current = 'sleep';
      },
    },
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
        options={speed.options}
        onClose={close}
      />
      <ActionSheet
        testID="player-sleep-sheet"
        visible={visible === 'sleep'}
        title="Sleep timer"
        subtitle={sleep.subtitle}
        options={sleep.options}
        onClose={close}
      />
    </>
  );
}
