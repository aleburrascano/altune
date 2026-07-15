/**
 * appendNativeTrack / insertNativeTrackNext — the Add to Queue / Play Next
 * native ops. Append maps to TrackPlayer.add(track) (end of queue); Play Next
 * maps to TrackPlayer.add(track, position) so it lands right after the active
 * track. The active track is never removed or reindexed, so audio continues.
 *
 * Local track-player mock with stable jest.fns (the shared manual mock returns
 * a fresh fn per access). Preview-source tracks skip the auth-header path.
 */

import type { PlaybackTrack } from '@shared/playback/types';

jest.mock('react-native-track-player', () => ({
  __esModule: true,
  default: {
    setupPlayer: jest.fn().mockResolvedValue(undefined),
    updateOptions: jest.fn().mockResolvedValue(undefined),
    add: jest.fn().mockResolvedValue(undefined),
  },
  Capability: new Proxy({}, { get: (_t: unknown, p: string | symbol) => String(p) }),
}));

import TrackPlayer from 'react-native-track-player';
import { appendNativeTrack, insertNativeTrackNext } from '../loadNativeTrack';

const mockTrackPlayer = TrackPlayer as unknown as {
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

describe('appendNativeTrack', () => {
  beforeEach(() => {
    mockTrackPlayer.add.mockClear();
  });

  it('adds the track with no insert index (appends at end)', async () => {
    await appendNativeTrack(preview('z'));

    expect(mockTrackPlayer.add).toHaveBeenCalledTimes(1);
    const call = mockTrackPlayer.add.mock.calls[0]!;
    expect(call[0]).toMatchObject({ title: 'z' });
    // no second argument => append at end
    expect(call[1]).toBeUndefined();
  });
});

describe('insertNativeTrackNext', () => {
  beforeEach(() => {
    mockTrackPlayer.add.mockClear();
  });

  it('adds the track before the given position (right after current)', async () => {
    await insertNativeTrackNext(preview('z'), 3);

    expect(mockTrackPlayer.add).toHaveBeenCalledTimes(1);
    const call = mockTrackPlayer.add.mock.calls[0]!;
    expect(call[0]).toMatchObject({ title: 'z' });
    expect(call[1]).toBe(3);
  });
});
