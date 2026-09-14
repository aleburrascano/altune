import { usePlayback } from '@shared/playback/usePlayback';
import type { ActionSheetOption } from '@shared/ui/primitives/ActionSheet';

import { PLAYBACK_RATES, rateLabel, usePlaybackRateStore } from '../playbackRateStore';

export type SpeedOptions = {
  /** Current rate, shown on the root menu row. */
  valueLabel: string;
  options: ActionSheetOption[];
};

export function useSpeedOptions(): SpeedOptions {
  const { setRate: applyRate } = usePlayback();
  const rate = usePlaybackRateStore((s) => s.rate);
  const setStoredRate = usePlaybackRateStore((s) => s.setRate);

  const options: ActionSheetOption[] = PLAYBACK_RATES.map((r) => ({
    label: r === rate ? `${rateLabel(r)}  ✓` : rateLabel(r),
    testID: `player-rate-${r}`,
    onPress: () => {
      setStoredRate(r);
      applyRate(r);
    },
  }));

  return { valueLabel: rateLabel(rate), options };
}
