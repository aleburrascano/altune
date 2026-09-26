import React from 'react';
import { fireEvent, render, screen } from '@testing-library/react-native';
import type { ReactTestInstance } from 'react-test-renderer';

import { asTrackId } from '@shared/api-client/ids';
import { PlaybackContext } from '@shared/playback/PlaybackContext';
import { useQueueStore } from '@shared/playback/queueStore';
import type { PlaybackContextValue } from '@shared/playback/types';

import { libraryTrack } from '../../__tests__/fixtures';
import { PlayerBar } from '../PlayerBar';

const mockPush = jest.fn();

jest.mock('expo-router', () => ({
  useRouter: () => ({ push: mockPush, back: jest.fn(), replace: jest.fn() }),
}));

const TRACK = libraryTrack();
const NEXT_TRACK = libraryTrack({ source: { kind: 'library', trackId: asTrackId('trk-2') } });

function idleFixture(): PlaybackContextValue {
  return {
    status: 'idle',
    track: null,
    positionMs: 0,
    durationMs: 0,
    errorMessage: null,
    errorKind: null,
    play: jest.fn(),
    startQueue: jest.fn(),
    skipToQueueIndex: jest.fn(),
    reorderUpcoming: jest.fn(),
    appendToQueue: jest.fn(),
    insertNext: jest.fn(),
    skipNext: jest.fn(),
    skipPrevious: jest.fn(),
    removeQueueIndex: jest.fn(),
    pause: jest.fn(),
    resume: jest.fn(),
    seekTo: jest.fn(),
    setRate: jest.fn(),
    stop: jest.fn(),
    retry: jest.fn(),
  };
}

function renderBar(value: Partial<PlaybackContextValue>) {
  const merged = { ...idleFixture(), ...value };
  render(
    <PlaybackContext.Provider value={merged}>
      <PlayerBar />
    </PlaybackContext.Provider>,
  );
  return merged;
}

beforeEach(() => {
  mockPush.mockClear();
  useQueueStore.getState().loadQueue([TRACK, NEXT_TRACK], 0, null);
});

afterEach(() => {
  useQueueStore.getState().clearQueue();
});

describe('PlayerBar — idle state', () => {
  it('shows a placeholder instead of disappearing when nothing is playing', () => {
    renderBar({});

    expect(screen.getByText('Pick something to play')).toBeTruthy();
  });
});

describe('PlayerBar — playing controls', () => {
  it('pauses a playing track', () => {
    const controls = renderBar({ status: 'playing', track: TRACK });

    fireEvent.press(screen.getByLabelText('Pause'));

    expect(controls.pause).toHaveBeenCalledTimes(1);
  });

  it('resumes a paused track', () => {
    const controls = renderBar({ status: 'paused', track: TRACK });

    fireEvent.press(screen.getByLabelText('Play'));

    expect(controls.resume).toHaveBeenCalledTimes(1);
  });

  it('skips to the next track', () => {
    const controls = renderBar({ status: 'playing', track: TRACK });

    fireEvent.press(screen.getByLabelText('Next track'));

    expect(controls.skipNext).toHaveBeenCalledTimes(1);
  });

  it('seeks through the scrubber', () => {
    const controls = renderBar({ status: 'playing', track: TRACK, positionMs: 30000, durationMs: 200000 });

    fireEvent(screen.getByLabelText(/^Playback position/), 'accessibilityAction', {
      nativeEvent: { actionName: 'increment' },
    });

    expect(controls.seekTo).toHaveBeenCalledWith(45000);
  });
});

describe('PlayerBar — the action offered for a failed track', () => {
  it('offers a retry for a network failure, which may succeed on a second try', () => {
    const controls = renderBar({
      status: 'error',
      track: TRACK,
      errorMessage: 'Could not load this track',
      errorKind: 'network',
    });

    fireEvent.press(screen.getByLabelText('Retry') as ReactTestInstance);

    expect(screen.queryByLabelText('Skip track')).toBeNull();
    expect(controls.retry).toHaveBeenCalledTimes(1);
  });

  it('offers a skip instead of a retry for a track that is permanently gone', () => {
    const controls = renderBar({
      status: 'error',
      track: TRACK,
      errorMessage: 'Could not load this track',
      errorKind: 'not_found',
    });

    fireEvent.press(screen.getByLabelText('Skip track') as ReactTestInstance);

    expect(screen.queryByLabelText('Retry')).toBeNull();
    expect(controls.skipNext).toHaveBeenCalledTimes(1);
  });
});

describe('PlayerBar — queue and lyrics', () => {
  it('opens the queue page', () => {
    renderBar({ status: 'playing', track: TRACK });

    fireEvent.press(screen.getByLabelText('View queue'));

    expect(mockPush).toHaveBeenCalledWith('/player/queue');
  });

  it('opens the lyrics page', () => {
    renderBar({ status: 'playing', track: TRACK });

    fireEvent.press(screen.getByLabelText('View lyrics'));

    expect(mockPush).toHaveBeenCalledWith('/player/lyrics');
  });
});
