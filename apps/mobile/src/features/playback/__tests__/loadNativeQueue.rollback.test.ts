// Regression for #812: a multi-track TrackPlayer.add that throws partway can leave
// a partial native queue out of step with queueStore, so later index-based skips and
// removes hit the wrong native item. loadNativeQueue must reset the native queue
// before the add error propagates.

import TrackPlayer from 'react-native-track-player';

import type { PlaybackTrack } from '@shared/playback/types';

import { loadNativeQueue } from '../loadNativeTrack';
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

function lastCallOrder(fn: unknown): number {
  const order = (fn as jest.Mock).mock.invocationCallOrder;
  return order[order.length - 1] ?? -1;
}

describe('loadNativeQueue — failed multi-track add rolls back the native queue', () => {
  it('resets the native queue after the add throws, then rethrows the add error', async () => {
    const addError = new Error('bridge dropped after 3 of 5 tracks');
    __player.failNext('add', addError);

    await expect(loadNativeQueue(makeTracks(5), 2, { autoplay: false })).rejects.toBe(addError);

    expect(__player.calls('add')).toHaveLength(1);
    expect(lastCallOrder(TrackPlayer.reset)).toBeGreaterThan(lastCallOrder(TrackPlayer.add));
    expect(__player.calls('skip')).toHaveLength(0);
    expect(__player.calls('play')).toHaveLength(0);
  });

  it('surfaces the add error even when the rollback reset also fails', async () => {
    const addError = new Error('add failed');
    // The pre-load reset has already succeeded by the time add runs; arm the
    // rollback reset to fail from inside add.
    (TrackPlayer.add as jest.Mock).mockImplementationOnce(async () => {
      __player.failNext('reset', new Error('reset failed'));
      throw addError;
    });

    await expect(loadNativeQueue(makeTracks(3), 0, { autoplay: false })).rejects.toBe(addError);
    expect(lastCallOrder(TrackPlayer.reset)).toBeGreaterThan(lastCallOrder(TrackPlayer.add));
  });

  it('skips the rollback when a newer load superseded it before the add rejected', async () => {
    const addError = new Error('late add rejection');
    // Models an add that outlived the lock deadline: a newer load claims the player
    // (and would rebuild the queue) before this add finally rejects.
    (TrackPlayer.add as jest.Mock).mockImplementationOnce(async () => {
      claimLoad();
      throw addError;
    });

    await expect(loadNativeQueue(makeTracks(3), 0, { autoplay: false })).rejects.toBe(addError);
    expect(lastCallOrder(TrackPlayer.reset)).toBeLessThan(lastCallOrder(TrackPlayer.add));
  });

  it('does not reset again when the add succeeds', async () => {
    await loadNativeQueue(makeTracks(3), 1, { autoplay: false });

    expect(__player.calls('reset')).toHaveLength(1);
    expect(lastCallOrder(TrackPlayer.skip)).toBeGreaterThan(lastCallOrder(TrackPlayer.add));
  });
});
