import { minutesRemaining, useSleepTimerStore } from '../sleepTimerStore';

const T0 = 1_700_000_000_000;
const THIRTY_MIN_MS = 30 * 60_000;

afterEach(() => {
  useSleepTimerStore.getState().cancel();
  jest.useRealTimers();
});

describe('sleep-timer write path with an injected clock', () => {
  it('bakes the injected now into endsAt and reads it back down to expiry', () => {
    useSleepTimerStore.getState().start(30, T0);

    const { endsAt } = useSleepTimerStore.getState();
    expect(endsAt).toBe(T0 + THIRTY_MIN_MS);
    expect(minutesRemaining(endsAt, T0)).toBe(30);
    expect(minutesRemaining(endsAt, T0 + 10 * 60_000)).toBe(20);
    expect(minutesRemaining(endsAt, T0 + THIRTY_MIN_MS)).toBe(0);
    expect(minutesRemaining(endsAt, T0 + THIRTY_MIN_MS + 60_000)).toBe(0);
  });
});
