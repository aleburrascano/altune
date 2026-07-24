import { create } from 'zustand';

export type SleepTimerState = {
  endsAt: number | null;
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

export function minutesRemaining(endsAt: number | null, now: number): number {
  if (endsAt === null) return 0;
  return Math.max(0, Math.ceil((endsAt - now) / 60_000));
}
