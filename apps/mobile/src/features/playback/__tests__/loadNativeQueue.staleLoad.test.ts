import TrackPlayer from 'react-native-track-player';

import type { PlaybackTrack } from '@shared/playback/types';

import { loadNativeQueue, loadNativeTrack, reorderUpcomingNative } from '../loadNativeTrack';
import { claimLoad } from '../loadToken';

import { previewTrack } from './fixtures';

const { __player } = jest.requireMock('react-native-track-player');

function makeTracks(count: number): PlaybackTrack[] {
  return Array.from({ length: count }, (_, i) =>
    previewTrack({
      source: { kind: 'preview', previewUrl: `https://cdn.example/${i}.mp3` },
      title: `Track ${i}`,
    }),
  );
}

function supersedeDuring(fn: unknown): void {
  (fn as jest.Mock).mockImplementationOnce(async () => {
    claimLoad();
  });
}

describe('a native op that outlived the lock deadline stops touching the player once a newer load claimed the token (#2527)', () => {
  it('queue load: no skip, seek or play after a newer load claimed the token during add', async () => {
    supersedeDuring(TrackPlayer.add);

    await loadNativeQueue(makeTracks(3), 2, { startPositionMs: 5000 });

    expect(__player.calls('skip')).toHaveLength(0);
    expect(__player.calls('seekTo')).toHaveLength(0);
    expect(__player.calls('play')).toHaveLength(0);
  });

  it('queue load: no seek or play after a newer load claimed the token during skip', async () => {
    supersedeDuring(TrackPlayer.skip);

    await loadNativeQueue(makeTracks(3), 2, { startPositionMs: 5000 });

    expect(__player.calls('seekTo')).toHaveLength(0);
    expect(__player.calls('play')).toHaveLength(0);
  });

  it('queue load: no play after a newer load claimed the token during seekTo', async () => {
    supersedeDuring(TrackPlayer.seekTo);

    await loadNativeQueue(makeTracks(3), 0, { startPositionMs: 5000 });

    expect(__player.calls('play')).toHaveLength(0);
  });

  it('single-track load: no seek or play after a newer load claimed the token during add', async () => {
    supersedeDuring(TrackPlayer.add);

    await loadNativeTrack(makeTracks(1)[0]!, { startPositionMs: 5000 });

    expect(__player.calls('seekTo')).toHaveLength(0);
    expect(__player.calls('play')).toHaveLength(0);
  });

  it('tail rebuild: no add after a newer load claimed the token during the active-track lookup', async () => {
    const getActive = TrackPlayer.getActiveTrack as jest.Mock;
    getActive.mockImplementationOnce(async () => undefined);
    supersedeDuring(getActive);

    await reorderUpcomingNative(makeTracks(3));

    expect(__player.calls('removeUpcomingTracks')).toHaveLength(0);
    expect(__player.calls('add')).toHaveLength(0);
  });

  it('tail rebuild: no add after a newer load claimed the token during removeUpcomingTracks', async () => {
    supersedeDuring(TrackPlayer.removeUpcomingTracks);

    await reorderUpcomingNative(makeTracks(3));

    expect(__player.calls('add')).toHaveLength(0);
  });
});
