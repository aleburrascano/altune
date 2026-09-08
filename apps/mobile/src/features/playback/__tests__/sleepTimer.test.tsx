import React from 'react';
import { act, render } from '@testing-library/react-native';

import { PlaybackContext } from '@shared/playback/PlaybackContext';
import type { PlaybackContextValue } from '@shared/playback/types';

import { minutesRemaining, useSleepTimerStore } from '../sleepTimerStore';
import { SleepTimerBridge } from '../ui/SleepTimerBridge';

const T0 = 1_700_000_000_000;
const THIRTY_MIN_MS = 30 * 60_000;

function renderBridge(pause: () => void, now: () => number): void {
  const value = { pause } as unknown as PlaybackContextValue;
  render(
    <PlaybackContext.Provider value={value}>
      <SleepTimerBridge now={now} />
    </PlaybackContext.Provider>,
  );
}

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

describe('SleepTimerBridge under a controlled clock', () => {
  it('fires at the deadline, pausing and clearing the timer', () => {
    jest.useFakeTimers();
    let clock = T0;
    const pause = jest.fn();

    useSleepTimerStore.getState().start(30, clock);
    renderBridge(pause, () => clock);

    expect(pause).not.toHaveBeenCalled();

    clock = T0 + THIRTY_MIN_MS;
    act(() => {
      jest.advanceTimersByTime(THIRTY_MIN_MS);
    });

    expect(pause).toHaveBeenCalledTimes(1);
    expect(useSleepTimerStore.getState().endsAt).toBeNull();
  });

  it('fires immediately when the injected clock is already past the deadline', () => {
    const pause = jest.fn();
    useSleepTimerStore.getState().start(30, T0);

    renderBridge(pause, () => T0 + THIRTY_MIN_MS);

    expect(pause).toHaveBeenCalledTimes(1);
    expect(useSleepTimerStore.getState().endsAt).toBeNull();
  });
});
