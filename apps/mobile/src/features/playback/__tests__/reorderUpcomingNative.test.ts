/**
 * reorderUpcomingNative — the seamless-shuffle native op. It must replace the
 * upcoming tracks (removeUpcomingTracks + add) WITHOUT touching the active
 * track. Order matters: remove before add, or the old tail lingers / dupes.
 *
 * Uses a local track-player mock with stable jest.fns so calls are assertable
 * (the shared manual mock returns a fresh fn per access). Preview-source tracks
 * are used so the auth-header path is skipped.
 */

import type { PlaybackTrack } from '@shared/playback/types';

// Define the stubs INSIDE the factory: a top-level `const` referenced from the
// factory is still in its TDZ when the mocked module is first required (imports
// hoist above it), which would leave `default` undefined.
jest.mock('react-native-track-player', () => ({
  __esModule: true,
  default: {
    setupPlayer: jest.fn().mockResolvedValue(undefined),
    updateOptions: jest.fn().mockResolvedValue(undefined),
    removeUpcomingTracks: jest.fn().mockResolvedValue(undefined),
    add: jest.fn().mockResolvedValue(undefined),
  },
  Capability: new Proxy({}, { get: (_t: unknown, p: string | symbol) => String(p) }),
}));

import TrackPlayer from 'react-native-track-player';
import { reorderUpcomingNative } from '../loadNativeTrack';

const mockTrackPlayer = TrackPlayer as unknown as {
  removeUpcomingTracks: jest.Mock;
  add: jest.Mock;
};

function preview(title: string): PlaybackTrack {
  return {
    source: { kind: 'preview', previewUrl: `https://example.com/${title}.mp3` },
    title,
    artist: `${title}-artist`,
    artworkUrl: null,
  };
}

describe('reorderUpcomingNative', () => {
  beforeEach(() => {
    mockTrackPlayer.removeUpcomingTracks.mockClear();
    mockTrackPlayer.add.mockClear();
  });

  it('removes the upcoming tracks then adds the new order', async () => {
    await reorderUpcomingNative([preview('b'), preview('c'), preview('d')]);

    expect(mockTrackPlayer.removeUpcomingTracks).toHaveBeenCalledTimes(1);
    expect(mockTrackPlayer.add).toHaveBeenCalledTimes(1);

    const [added] = mockTrackPlayer.add.mock.calls[0]!;
    expect(added.map((t: { title: string }) => t.title)).toEqual(['b', 'c', 'd']);

    // remove must precede add, otherwise the old tail is duplicated.
    const removeOrder = mockTrackPlayer.removeUpcomingTracks.mock.invocationCallOrder[0]!;
    const addOrder = mockTrackPlayer.add.mock.invocationCallOrder[0]!;
    expect(removeOrder).toBeLessThan(addOrder);
  });

  it('clears the upcoming tracks and adds nothing when the tail is empty', async () => {
    await reorderUpcomingNative([]);

    expect(mockTrackPlayer.removeUpcomingTracks).toHaveBeenCalledTimes(1);
    expect(mockTrackPlayer.add).not.toHaveBeenCalled();
  });
});
