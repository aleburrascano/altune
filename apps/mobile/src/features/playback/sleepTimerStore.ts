import { create } from 'zustand';

export const monotonicNow = (): number => performance.now();

export type SleepTimerState = {
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

export function minutesRemaining(endsAt: number | null, now: number): number {
  if (endsAt === null) return 0;
  return Math.max(0, Math.ceil((endsAt - now) / 60_000));
}
