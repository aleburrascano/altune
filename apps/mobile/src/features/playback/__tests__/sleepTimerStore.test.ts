import { minutesRemaining, useSleepTimerStore } from '../sleepTimerStore';

beforeEach(() => {
  useSleepTimerStore.getState().cancel();
});

describe('minutesRemaining', () => {
  it('is 0 when no timer is set', () => {
    expect(minutesRemaining(null, 1_000)).toBe(0);
  });

  // Rounded up: a timer with 30 seconds left must not read "0 min" while it is
  // still going to fire.
  it('rounds partial minutes up', () => {
    expect(minutesRemaining(1_030_000, 1_000_000)).toBe(1);
  });

  it('clamps to 0 once the deadline has passed', () => {
    expect(minutesRemaining(1_000, 60_000)).toBe(0);
  });
});

describe('sleep timer store', () => {
  it('stores an absolute deadline so a backgrounded countdown cannot drift', () => {
    const before = Date.now();
    useSleepTimerStore.getState().start(30);
    const { endsAt, minutes } = useSleepTimerStore.getState();

    expect(minutes).toBe(30);
    expect(endsAt).not.toBeNull();
    expect(endsAt as number).toBeGreaterThanOrEqual(before + 30 * 60_000);
  });

  it('cancel clears both the deadline and the label', () => {
    useSleepTimerStore.getState().start(15);
    useSleepTimerStore.getState().cancel();

    expect(useSleepTimerStore.getState().endsAt).toBeNull();
    expect(useSleepTimerStore.getState().minutes).toBeNull();
  });
});
