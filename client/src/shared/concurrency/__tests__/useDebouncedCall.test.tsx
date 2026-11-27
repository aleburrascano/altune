import { act, renderHook } from '@testing-library/react-native';

import { useDebouncedCall } from '@shared/concurrency';

const DELAY_MS = 300;

beforeEach(() => {
  jest.useFakeTimers();
});

afterEach(() => {
  jest.useRealTimers();
});

describe('useDebouncedCall', () => {
  it('runs only the last scheduled call, after the delay', () => {
    const { result } = renderHook(() => useDebouncedCall(DELAY_MS));
    const first = jest.fn();
    const last = jest.fn();

    act(() => {
      result.current.schedule(first);
      result.current.schedule(last);
      jest.advanceTimersByTime(DELAY_MS);
    });

    expect(first).not.toHaveBeenCalled();
    expect(last).toHaveBeenCalledTimes(1);
  });

  it('does not run a call that was cancelled', () => {
    const { result } = renderHook(() => useDebouncedCall(DELAY_MS));
    const run = jest.fn();

    act(() => {
      result.current.schedule(run);
      result.current.cancel();
      jest.advanceTimersByTime(DELAY_MS);
    });

    expect(run).not.toHaveBeenCalled();
  });

  it('does not run a pending call after the component unmounts', () => {
    const { result, unmount } = renderHook(() => useDebouncedCall(DELAY_MS));
    const run = jest.fn();

    act(() => {
      result.current.schedule(run);
    });
    unmount();
    act(() => {
      jest.advanceTimersByTime(DELAY_MS);
    });

    expect(run).not.toHaveBeenCalled();
  });
});
