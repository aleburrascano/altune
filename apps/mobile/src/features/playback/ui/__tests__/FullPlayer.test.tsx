import React from 'react';
import { fireEvent, render, screen } from '@testing-library/react-native';
import type { ReactTestInstance } from 'react-test-renderer';

import { asTrackId } from '@shared/api-client/ids';
import { PlaybackContext } from '@shared/playback/PlaybackContext';
import { useQueueStore } from '@shared/playback/queueStore';
import type { PlaybackContextValue, PlaybackErrorKind } from '@shared/playback/types';

import { libraryTrack } from '../../__tests__/fixtures';
import { FullPlayer } from '../FullPlayer';

jest.mock('expo-router', () => ({
  useRouter: () => ({ push: jest.fn(), back: jest.fn(), replace: jest.fn() }),
}));

const FAILED_TRACK = libraryTrack();
const NEXT_TRACK = libraryTrack({ source: { kind: 'library', trackId: asTrackId('trk-2') } });

function renderPlayerInError(
  Player: () => React.JSX.Element | null,
  errorKind: PlaybackErrorKind,
): { retry: jest.Mock; skipNext: jest.Mock } {
  const commands = { retry: jest.fn(), skipNext: jest.fn() };
  const value = {
    ...commands,
    status: 'error',
    track: FAILED_TRACK,
    positionMs: 0,
    durationMs: 0,
    errorMessage: 'Could not load this track',
    errorKind,
  } as unknown as PlaybackContextValue;

  render(
    <PlaybackContext.Provider value={value}>
      <Player />
    </PlaybackContext.Provider>,
  );
  return commands;
}

beforeEach(() => {
  useQueueStore.getState().loadQueue([FAILED_TRACK, NEXT_TRACK], 0, null);
});

afterEach(() => {
  useQueueStore.getState().clearQueue();
});

// The full player names its call to action by printing the label on a text button.
const PLAYERS = [
  { name: 'FullPlayer', Player: FullPlayer, cta: (label: string) => screen.queryByText(label) },
];

describe.each(PLAYERS)('$name — the action offered for a failed track', ({ Player, cta }) => {
  it('offers a retry for a network failure, which may succeed on a second try', () => {
    const { retry } = renderPlayerInError(Player, 'network');

    fireEvent.press(cta('Retry') as ReactTestInstance);

    expect(cta('Skip track')).toBeNull();
    expect(retry).toHaveBeenCalledTimes(1);
  });

  it('offers a skip instead of a retry for a track that is permanently gone', () => {
    const { skipNext } = renderPlayerInError(Player, 'not_found');

    fireEvent.press(cta('Skip track') as ReactTestInstance);

    expect(cta('Retry')).toBeNull();
    expect(skipNext).toHaveBeenCalledTimes(1);
  });
});

describe('FullPlayer — a permanently gone track at the end of the queue', () => {
  it('offers neither a retry nor a skip when there is nothing to skip to', () => {
    useQueueStore.getState().loadQueue([FAILED_TRACK], 0, null);

    renderPlayerInError(FullPlayer, 'not_found');

    expect(screen.queryByText('Retry')).toBeNull();
    expect(screen.queryByText('Skip track')).toBeNull();
  });
});

describe('FullPlayer — transport controls', () => {
  function renderPlaying(overrides: Partial<PlaybackContextValue>) {
    const value = {
      status: 'playing',
      track: FAILED_TRACK,
      positionMs: 30000,
      durationMs: 200000,
      errorMessage: null,
      errorKind: null,
      seekTo: jest.fn(),
      skipPrevious: jest.fn(),
      skipNext: jest.fn(),
      pause: jest.fn(),
      resume: jest.fn(),
      reorderUpcoming: jest.fn(),
      ...overrides,
    } as unknown as PlaybackContextValue;
    render(
      <PlaybackContext.Provider value={value}>
        <FullPlayer />
      </PlaybackContext.Provider>,
    );
    return value;
  }

  it('restarts the current track when Previous is pressed 30 seconds in', () => {
    useQueueStore.getState().loadQueue([NEXT_TRACK, FAILED_TRACK], 1, null);
    const controls = renderPlaying({ positionMs: 30000 });

    fireEvent.press(screen.getByLabelText('Previous track'));

    expect(controls.seekTo).toHaveBeenCalledWith(0);
    expect(controls.skipPrevious).not.toHaveBeenCalled();
  });

  it('goes back a track when Previous is pressed in the first second', () => {
    useQueueStore.getState().loadQueue([NEXT_TRACK, FAILED_TRACK], 1, null);
    const controls = renderPlaying({ positionMs: 1000 });

    fireEvent.press(screen.getByLabelText('Previous track'));

    expect(controls.skipPrevious).toHaveBeenCalledTimes(1);
    expect(controls.seekTo).not.toHaveBeenCalled();
  });

  it('pauses a playing track and skips to the next one', () => {
    const controls = renderPlaying({});

    fireEvent.press(screen.getByLabelText('Pause'));
    fireEvent.press(screen.getByLabelText('Next track'));

    expect(controls.pause).toHaveBeenCalledTimes(1);
    expect(controls.skipNext).toHaveBeenCalledTimes(1);
  });

  it('disables Next on the last track of the queue', () => {
    useQueueStore.getState().loadQueue([FAILED_TRACK], 0, null);
    const controls = renderPlaying({});

    const next = screen.getByLabelText('Next track');
    fireEvent.press(next);

    expect(next.props.accessibilityState).toEqual({ disabled: true });
    expect(controls.skipNext).not.toHaveBeenCalled();
  });

  it('shuffles the queue and cycles repeat from off to all to one', () => {
    renderPlaying({});

    fireEvent.press(screen.getByLabelText('Enable shuffle'));
    fireEvent.press(screen.getByLabelText('Repeat: off'));
    fireEvent.press(screen.getByLabelText('Repeat: all'));

    expect(useQueueStore.getState().shuffled).toBe(true);
    expect(useQueueStore.getState().repeatMode).toBe('one');
    expect(screen.getByLabelText('Repeat: one')).toBeTruthy();
  });
});
