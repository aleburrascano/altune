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
    reorderUpcoming: jest.fn().mockResolvedValue(undefined),
    appendToQueue: jest.fn().mockResolvedValue(undefined),
    insertNext: jest.fn().mockResolvedValue(undefined),
    skipToQueueIndex: jest.fn().mockResolvedValue(undefined),
    skipNext: jest.fn().mockResolvedValue(undefined),
    skipPrevious: jest.fn().mockResolvedValue(undefined),
    removeQueueIndex: jest.fn().mockResolvedValue(undefined),
    pause: noop,
    resume: noop,
    seekTo: noop,
    setRate: noop,
    stop: noop,
    retry: noop,
    ...overrides,
  };
}

describe('FullPlayer shuffle', () => {
  it('reorders the native upcoming queue when shuffle is toggled', () => {
    const first = _track('a');
    const tracks = [first, _track('b'), _track('c'), _track('d')];
    useQueueStore.getState().loadQueue(tracks, 0, { kind: 'library' });

    const reorderUpcoming = jest.fn().mockResolvedValue(undefined);
    const value = _contextValue({ track: first, reorderUpcoming });

    const { getByLabelText } = render(
      <PlaybackContext.Provider value={value}>
        <FullPlayer />
      </PlaybackContext.Provider>,
    );

    fireEvent.press(getByLabelText('Enable shuffle'));

    expect(useQueueStore.getState().shuffled).toBe(true);
    expect(reorderUpcoming).toHaveBeenCalledTimes(1);
    const [upcoming] = reorderUpcoming.mock.calls[0];
    expect(upcoming.map((t: PlaybackTrack) => t.title).sort()).toEqual(['b', 'c', 'd']);
  });
});
