/**
 * The chosen playback rate, kept in a store rather than screen state so the
 * label survives the player being closed and reopened. Applying it to the native
 * player is the caller's job (`usePlayback().setRate`) — this only remembers the
 * choice.
 */
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

/** "1×" / "1.5×" — trailing zeros trimmed so 1.50× never appears. */
export function rateLabel(rate: number): string {
  return `${String(rate)}×`;
}
