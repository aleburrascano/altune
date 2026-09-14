import React from 'react';
import { act, fireEvent, render, screen } from '@testing-library/react-native';

import { PlaybackContext } from '@shared/playback/PlaybackContext';
import type { PlaybackContextValue } from '@shared/playback/types';

import { usePlaybackRateStore } from '../playbackRateStore';
import { useSleepTimerStore } from '../sleepTimerStore';
import { PlayerOptionsSheets } from '../ui/PlayerOptionsSheets';

function renderSheets(setRate = jest.fn(), onClose = jest.fn()): void {
  const value = { setRate } as unknown as PlaybackContextValue;
  render(
    <PlaybackContext.Provider value={value}>
      <PlayerOptionsSheets open onClose={onClose} />
    </PlaybackContext.Provider>,
  );
}

function openSubmenu(testID: string): void {
  fireEvent.press(screen.getByTestId(testID));
}

afterEach(() => {
  useSleepTimerStore.getState().cancel();
  usePlaybackRateStore.getState().setRate(1);
  jest.useRealTimers();
});

describe('PlayerOptionsSheets', () => {
  it('shows current speed and sleep state on the root menu', () => {
    usePlaybackRateStore.getState().setRate(1.5);
    renderSheets();

    expect(screen.getByLabelText('Playback speed · 1.5×')).toBeTruthy();
    expect(screen.getByLabelText('Sleep timer · Off')).toBeTruthy();
  });

  it('navigates to the speed sheet, marks the current rate, and applies a choice', () => {
    const setRate = jest.fn();
    const onClose = jest.fn();
    renderSheets(setRate, onClose);

    openSubmenu('player-options-speed');

    expect(screen.getByTestId('player-speed-sheet').props.visible).toBe(true);
    expect(screen.queryByTestId('player-options-sheet')).toBeNull();
    expect(screen.getByLabelText('1×  ✓')).toBeTruthy();
    for (const r of [0.75, 1.25, 1.5, 1.75, 2]) {
      expect(screen.getByTestId(`player-rate-${r}`)).toBeTruthy();
    }

    fireEvent.press(screen.getByTestId('player-rate-1.25'));

    expect(usePlaybackRateStore.getState().rate).toBe(1.25);
    expect(setRate).toHaveBeenCalledWith(1.25);
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('starts a sleep timer and then offers a live countdown and turn-off', () => {
    jest.useFakeTimers();
    jest.setSystemTime(1_700_000_000_000);
    renderSheets();

    openSubmenu('player-options-sleep');
    expect(screen.getByTestId('player-sleep-sheet').props.visible).toBe(true);
    expect(screen.queryByTestId('player-sleep-off')).toBeNull();
    for (const m of [15, 30, 45, 60]) {
      expect(screen.getByLabelText(`${m} minutes`)).toBeTruthy();
    }

    fireEvent.press(screen.getByTestId('player-sleep-30'));
    expect(useSleepTimerStore.getState().minutes).toBe(30);

    act(() => {
      jest.advanceTimersByTime(10 * 60_000);
    });
    expect(screen.getByLabelText('Sleep timer · 20 min left')).toBeTruthy();
    openSubmenu('player-options-sleep');

    expect(screen.getByText('Pausing in 20 min')).toBeTruthy();

    fireEvent.press(screen.getByTestId('player-sleep-off'));
    expect(useSleepTimerStore.getState().endsAt).toBeNull();
  });
});
