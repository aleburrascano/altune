import React from 'react';
import { fireEvent, render, screen } from '@testing-library/react-native';
import type { ReactTestInstance } from 'react-test-renderer';

import { asTrackId } from '@shared/api-client/ids';
import { PlaybackContext } from '@shared/playback/PlaybackContext';
import { useQueueStore } from '@shared/playback/queueStore';
import type { PlaybackContextValue, PlaybackErrorKind } from '@shared/playback/types';

import { libraryTrack } from '../../__tests__/fixtures';
import { FullPlayer } from '../FullPlayer';
import { MiniPlayer } from '../MiniPlayer';

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

// Each player names its call to action in its own idiom: the mini player labels an icon
// button, the full player prints the label on a text button.
const PLAYERS = [
  { name: 'MiniPlayer', Player: MiniPlayer, cta: (label: string) => screen.queryByLabelText(label) },
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
