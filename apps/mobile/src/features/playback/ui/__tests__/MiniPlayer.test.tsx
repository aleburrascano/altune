import React from 'react';
import { fireEvent, render, screen } from '@testing-library/react-native';
import type { ReactTestInstance } from 'react-test-renderer';

import { asTrackId } from '@shared/api-client/ids';
import { PlaybackContext } from '@shared/playback/PlaybackContext';
import { useQueueStore } from '@shared/playback/queueStore';
import type { PlaybackContextValue, PlaybackErrorKind } from '@shared/playback/types';

import { libraryTrack } from '../../__tests__/fixtures';
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

// The mini player names its call to action by labelling an icon button.
const PLAYERS = [
  {
    name: 'MiniPlayer',
    Player: MiniPlayer,
    cta: (label: string) => screen.queryByLabelText(label),
  },
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

let mockWindowWidth = 1440;

jest.mock('react-native/Libraries/Utilities/useWindowDimensions', () => ({
  __esModule: true,
  default: () => ({ width: mockWindowWidth, height: 900, scale: 2, fontScale: 1 }),
}));

describe('MiniPlayer — hidden behind the persistent bar in wide web layout', () => {
  const RN = require('react-native');
  const NATIVE_OS: string = RN.Platform.OS;

  afterEach(() => {
    RN.Platform.OS = NATIVE_OS;
    mockWindowWidth = 1440;
  });

  it('renders nothing on a wide web window even with a track playing', () => {
    RN.Platform.OS = 'web';
    const value = { status: 'playing', track: FAILED_TRACK } as unknown as PlaybackContextValue;

    render(
      <PlaybackContext.Provider value={value}>
        <MiniPlayer />
      </PlaybackContext.Provider>,
    );

    expect(screen.queryByTestId('mini-player')).toBeNull();
  });

  it('still renders on a narrow web window', () => {
    RN.Platform.OS = 'web';
    mockWindowWidth = 390;
    const value = { status: 'playing', track: FAILED_TRACK } as unknown as PlaybackContextValue;

    render(
      <PlaybackContext.Provider value={value}>
        <MiniPlayer />
      </PlaybackContext.Provider>,
    );

    expect(screen.getByTestId('mini-player')).toBeTruthy();
  });

  it('still renders on a wide native window', () => {
    RN.Platform.OS = NATIVE_OS;
    const value = { status: 'playing', track: FAILED_TRACK } as unknown as PlaybackContextValue;

    render(
      <PlaybackContext.Provider value={value}>
        <MiniPlayer />
      </PlaybackContext.Provider>,
    );

    expect(screen.getByTestId('mini-player')).toBeTruthy();
  });
});

beforeAll(() => {
  jest.useFakeTimers();
});

afterAll(() => {
  jest.useRealTimers();
});

describe('MiniPlayer — at the wide breakpoint on web', () => {
  const RN = require('react-native');
  const NATIVE_OS: string = RN.Platform.OS;

  afterEach(() => {
    RN.Platform.OS = NATIVE_OS;
    mockWindowWidth = 1440;
  });

  function renderPlaying() {
    const value = { status: 'playing', track: FAILED_TRACK } as unknown as PlaybackContextValue;
    render(
      <PlaybackContext.Provider value={value}>
        <MiniPlayer />
      </PlaybackContext.Provider>,
    );
  }

  it('renders nothing at exactly 1000px on web', () => {
    RN.Platform.OS = 'web';
    mockWindowWidth = 1000;

    renderPlaying();

    expect(screen.queryByTestId('mini-player')).toBeNull();
  });

  it('still renders at 999px on web', () => {
    RN.Platform.OS = 'web';
    mockWindowWidth = 999;

    renderPlaying();

    expect(screen.getByTestId('mini-player')).toBeTruthy();
  });

  it('still renders on a 390px phone', () => {
    RN.Platform.OS = NATIVE_OS;
    mockWindowWidth = 390;

    renderPlaying();

    expect(screen.getByTestId('mini-player')).toBeTruthy();
  });
});
