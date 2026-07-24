/**
 * Sleep timer — a wall-clock deadline after which playback pauses.
 *
 * Stores an absolute `endsAt` rather than a remaining duration so the countdown
 * survives the app being backgrounded: JS timers are throttled or suspended
 * there, and a decrementing counter would drift or freeze. The bridge
 * (`useSleepTimer`) re-derives the remaining time from the clock on every wake.
 */
import { create } from 'zustand';

export type SleepTimerState = {
  /** Epoch ms at which playback should pause, or null when no timer is set. */
  endsAt: number | null;
  /** The duration the user chose, kept for the label ("30 min"). */
  minutes: number | null;
  start: (minutes: number) => void;
  cancel: () => void;
};

export const useSleepTimerStore = create<SleepTimerState>((set) => ({
  endsAt: null,
  minutes: null,
  start: (minutes) => set({ endsAt: Date.now() + minutes * 60_000, minutes }),
  cancel: () => set({ endsAt: null, minutes: null }),
}));

/** Whole minutes left, rounded up so a timer never reads "0 min" while running.
 *  0 once the deadline has passed or no timer is set. */
export function minutesRemaining(endsAt: number | null, now: number): number {
  if (endsAt === null) return 0;
  return Math.max(0, Math.ceil((endsAt - now) / 60_000));
}
