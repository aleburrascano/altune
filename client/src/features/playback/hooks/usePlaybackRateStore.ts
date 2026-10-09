import { create } from 'zustand';

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
