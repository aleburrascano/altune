import { monotonicNow } from '@shared/timing';

const ELAPSED_MS = 250;
const WALL_CLOCK_JUMP_BACK_MS = 3_600_000;

beforeEach(() => {
  jest.useFakeTimers();
});

afterEach(() => {
  jest.useRealTimers();
});

describe('monotonicNow — a clock that never runs backwards', () => {
  it('advances by the time that passed', () => {
    const start = monotonicNow();
    jest.advanceTimersByTime(ELAPSED_MS);
    expect(monotonicNow() - start).toBe(ELAPSED_MS);
  });

  it('ignores the wall clock being set back', () => {
    const start = monotonicNow();
    jest.setSystemTime(Date.now() - WALL_CLOCK_JUMP_BACK_MS);
    expect(monotonicNow()).toBe(start);
  });
});
