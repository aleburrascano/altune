import React from 'react';
import { act, render } from '@testing-library/react-native';
import { AppState, type AppStateStatus } from 'react-native';

import { PlaybackContext } from '@shared/playback/PlaybackContext';
import type { PlaybackContextValue } from '@shared/playback/types';

import { useSleepTimerStore } from '../../sleepTimerStore';
import { SleepTimerBridge } from '../SleepTimerBridge';

const T0 = 1_700_000_000_000;
const THIRTY_MIN_MS = 30 * 60_000;

function renderBridge(pause: () => void, now: () => number): void {
  const value = { pause } as unknown as PlaybackContextValue;
  render(
    <PlaybackContext.Provider value={value}>
      <SleepTimerBridge monotonicNow={now} />
    </PlaybackContext.Provider>,
  );
}

afterEach(() => {
  useSleepTimerStore.getState().cancel();
  jest.useRealTimers();
});

describe('SleepTimerBridge under a controlled clock', () => {
  it('fires at the deadline, pausing and clearing the timer', () => {
    jest.useFakeTimers();
    let clock = T0;
    const pause = jest.fn();

    useSleepTimerStore.getState().start(30, clock, clock);
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
    useSleepTimerStore.getState().start(30, T0, T0);

    renderBridge(pause, () => T0 + THIRTY_MIN_MS);

    expect(pause).toHaveBeenCalledTimes(1);
    expect(useSleepTimerStore.getState().endsAt).toBeNull();
  });

  it('fires when the app returns to the foreground past the deadline', () => {
    jest.useFakeTimers();
    let listener: ((state: AppStateStatus) => void) | undefined;
    const spy = jest.spyOn(AppState, 'addEventListener').mockImplementation((_type, l) => {
      listener = l as (state: AppStateStatus) => void;
      return { remove: jest.fn() };
    });
    let clock = T0;
    const pause = jest.fn();

    useSleepTimerStore.getState().start(30, clock, clock);
    renderBridge(pause, () => clock);

    act(() => listener?.('active'));
    expect(pause).not.toHaveBeenCalled();

    clock = T0 + THIRTY_MIN_MS;
    act(() => listener?.('background'));
    expect(pause).not.toHaveBeenCalled();
    act(() => listener?.('active'));

    expect(pause).toHaveBeenCalledTimes(1);
    expect(useSleepTimerStore.getState().endsAt).toBeNull();
    spy.mockRestore();
  });
});

// Real default clocks under fake timers: jest.setSystemTime moves Date.now()
// without moving performance.now(), which is exactly a system clock jump.
describe('SleepTimerBridge across a system clock jump', () => {
  const DAY_MS = 24 * 60 * 60_000;

  let foreground: ((state: AppStateStatus) => void) | undefined;

  function renderDefaultBridge(pause: () => void): void {
    const value = { pause } as unknown as PlaybackContextValue;
    render(
      <PlaybackContext.Provider value={value}>
        <SleepTimerBridge />
      </PlaybackContext.Provider>,
    );
  }

  function elapse(ms: number): void {
    act(() => {
      jest.advanceTimersByTime(ms);
    });
  }

  beforeEach(() => {
    jest.useFakeTimers();
    jest.setSystemTime(T0);
    jest.spyOn(AppState, 'addEventListener').mockImplementation((_type, l) => {
      foreground = l as (state: AppStateStatus) => void;
      return { remove: jest.fn() };
    });
  });

  afterEach(() => {
    jest.restoreAllMocks();
  });

  it('does not fire early when the wall clock jumps forward', () => {
    const pause = jest.fn();
    useSleepTimerStore.getState().start(30);
    renderDefaultBridge(pause);

    elapse(60_000);
    jest.setSystemTime(Date.now() + 3 * 60 * 60_000);
    act(() => foreground?.('active'));
    elapse(28 * 60_000);
    expect(pause).not.toHaveBeenCalled();

    elapse(60_000);
    expect(pause).toHaveBeenCalledTimes(1);
    expect(useSleepTimerStore.getState().endsAt).toBeNull();
  });

  it('still fires on time when the wall clock jumps backward', () => {
    const pause = jest.fn();
    useSleepTimerStore.getState().start(30);
    jest.setSystemTime(T0 - 30 * DAY_MS);
    renderDefaultBridge(pause);

    elapse(29 * 60_000);
    expect(pause).not.toHaveBeenCalled();

    elapse(60_000);
    expect(pause).toHaveBeenCalledTimes(1);
    expect(useSleepTimerStore.getState().endsAt).toBeNull();
  });

  it('never arms a setTimeout beyond the safe 32-bit delay', () => {
    const timeoutSpy = jest.spyOn(global, 'setTimeout');
    const pause = jest.fn();
    useSleepTimerStore.getState().start(30 * 24 * 60);
    renderDefaultBridge(pause);

    elapse(60_000);
    expect(pause).not.toHaveBeenCalled();
    const delays = timeoutSpy.mock.calls.map((call) => call[1] ?? 0);
    expect(Math.max(...delays)).toBeLessThanOrEqual(2 ** 31 - 1);
  });
});
