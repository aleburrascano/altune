import { renderHook } from '@testing-library/react-native';

import { useSleepOptions, useSleepTimerStore } from '@features/playback/hooks/useSleepTimer';

const T0 = 1_700_000_000_000;
const THIRTY_MIN_MS = 30 * 60_000;

const TEN_MIN_MS = 600_000;
const ONE_MIN_MS = 60_000;

function minutesLeftAt(now: number): string {
  jest.useFakeTimers({ now });
  return renderHook(() => useSleepOptions()).result.current.valueLabel;
}

afterEach(() => {
  useSleepTimerStore.getState().cancel();
  jest.useRealTimers();
});

describe('sleep-timer write path with an injected clock', () => {
  it('bakes the injected now into endsAt and reads it back down to expiry', () => {
    useSleepTimerStore.getState().start(30, T0);

    expect(useSleepTimerStore.getState().endsAt).toBe(T0 + THIRTY_MIN_MS);
    expect(minutesLeftAt(T0)).toBe('30 min left');
    expect(minutesLeftAt(T0 + TEN_MIN_MS)).toBe('20 min left');
    expect(minutesLeftAt(T0 + THIRTY_MIN_MS)).toBe('0 min left');
    expect(minutesLeftAt(T0 + THIRTY_MIN_MS + ONE_MIN_MS)).toBe('0 min left');
  });
});
