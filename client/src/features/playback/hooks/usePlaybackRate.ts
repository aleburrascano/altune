import { create } from 'zustand';

import { usePlayback } from '@shared/playback/usePlayback';
import type { ActionSheetOption } from '@shared/ui/primitives/ActionSheet';

export const PLAYBACK_RATES = [0.75, 1, 1.25, 1.5, 1.75, 2] as const;

export type PlaybackRateState = {
  rate: number;
  setRate: (rate: number) => void;
};

export const usePlaybackRateStore = create<PlaybackRateState>((set) => ({
  rate: 1,
  setRate: (rate) => set({ rate }),
}));

export function rateLabel(rate: number): string {
  return `${String(rate)}×`;
}

export type SpeedOptions = {
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
