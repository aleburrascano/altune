/**
 * FullPlayer shuffle wiring — regression for the "UI jumps to a random song
 * while the correct song keeps playing" bug.
 *
 * The store's currentIndex is kept in lockstep with the native player via
 * syncCurrentIndex, which assumes native queue order == store playOrder. So a
 * shuffle from the UI MUST rebuild the native queue (startQueue), not just
 * reshuffle the store — otherwise the next native track-changed event maps its
 * index onto the freshly-shuffled playOrder and resolves to the wrong track.
 *
 * FullPlayer must therefore drive shuffle through useQueuePlayback (store
 * reshuffle + native rebuild), never the raw store toggleShuffle.
 */

import { render, fireEvent } from '@testing-library/react-native';

import { FullPlayer } from '../ui/FullPlayer';
import { PlaybackContext } from '@shared/playback/PlaybackContext';
import { useQueueStore } from '@shared/playback/queueStore';
import type { PlaybackContextValue, PlaybackTrack } from '@shared/playback/types';

jest.mock('expo-image', () => ({ Image: () => null }));
jest.mock('expo-router', () => ({
  useRouter: () => ({ push: jest.fn(), back: jest.fn() }),
}));

function _track(id: string): PlaybackTrack {
  return {
    source: { kind: 'library', trackId: id },
    title: id,
    artist: `${id}-artist`,
    artworkUrl: null,
    durationSeconds: 180,
  };
}

function _contextValue(overrides: Partial<PlaybackContextValue>): PlaybackContextValue {
  const noop = jest.fn();
  return {
    status: 'playing',
    track: _track('a'),
    positionMs: 1000,
    durationMs: 180000,
    errorMessage: null,
    play: jest.fn(),
    startQueue: jest.fn().mockResolvedValue(undefined),
    skipToQueueIndex: jest.fn().mockResolvedValue(undefined),
    skipNext: jest.fn().mockResolvedValue(undefined),
    skipPrevious: jest.fn().mockResolvedValue(undefined),
    removeQueueIndex: jest.fn().mockResolvedValue(undefined),
    pause: noop,
    resume: noop,
    seekTo: noop,
    stop: noop,
    retry: noop,
    ...overrides,
  };
}

describe('FullPlayer shuffle', () => {
  it('rebuilds the native queue when shuffle is toggled', () => {
    const first = _track('a');
    const tracks = [first, _track('b'), _track('c'), _track('d')];
    useQueueStore.getState().loadQueue(tracks, 0, { kind: 'library' });

    const startQueue = jest.fn().mockResolvedValue(undefined);
    const value = _contextValue({ track: first, startQueue });

    const { getByLabelText } = render(
      <PlaybackContext.Provider value={value}>
        <FullPlayer />
      </PlaybackContext.Provider>,
    );

    fireEvent.press(getByLabelText('Enable shuffle'));

    // The store must have shuffled...
    expect(useQueueStore.getState().shuffled).toBe(true);
    // ...AND the native queue must have been rebuilt to match, or the UI will
    // desync from audio on the next track-changed event.
    expect(startQueue).toHaveBeenCalledTimes(1);
  });
});
