import type { PlaybackTrack } from '@shared/playback/types';

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
