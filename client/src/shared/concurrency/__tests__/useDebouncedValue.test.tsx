import { act, renderHook } from '@testing-library/react-native';

import { useDebouncedValue } from '@shared/concurrency';

const DELAY_MS = 300;
const JUST_BEFORE_MS = 299;

type ValueProps = { value: string };

function renderDebounced(initial: string) {
  return renderHook(({ value }: ValueProps) => useDebouncedValue(value, DELAY_MS), {
    initialProps: { value: initial },
  });
}

beforeEach(() => {
  jest.useFakeTimers();
});

afterEach(() => {
  jest.useRealTimers();
});

describe('useDebouncedValue', () => {
  it('holds the first value and settles on a new one only after the delay', () => {
    const { result, rerender } = renderDebounced('a');

    rerender({ value: 'ab' });
    act(() => {
      jest.advanceTimersByTime(JUST_BEFORE_MS);
    });
    expect(result.current).toBe('a');

    act(() => {
      jest.advanceTimersByTime(1);
    });
    expect(result.current).toBe('ab');
  });

  it('restarts the wait on every change, so only the last value settles', () => {
    const { result, rerender } = renderDebounced('a');

    rerender({ value: 'ab' });
    act(() => {
      jest.advanceTimersByTime(JUST_BEFORE_MS);
    });
    rerender({ value: 'abc' });
    act(() => {
      jest.advanceTimersByTime(JUST_BEFORE_MS);
    });
    expect(result.current).toBe('a');

    act(() => {
      jest.advanceTimersByTime(1);
    });
    expect(result.current).toBe('abc');
  });
});
