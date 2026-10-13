import { monotonicNow } from '@shared/time';

const PERFORMANCE_NOW_MS = 1234.5;

describe('monotonicNow — a clock that never runs backwards', () => {
  it('reads the performance clock, not the wall clock', () => {
    const spy = jest.spyOn(performance, 'now').mockReturnValue(PERFORMANCE_NOW_MS);
    expect(monotonicNow()).toBe(PERFORMANCE_NOW_MS);
    spy.mockRestore();
  });
});
