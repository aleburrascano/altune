import { useQueueStore } from '@shared/playback/queueStore';
import { trackKey } from '@shared/playback/trackKey';
import type { PlaybackTrack } from '@shared/playback/types';

import { createNativePlaybackActions } from '../createNativePlaybackActions';
import { usePlaybackErrorStore } from '../playbackErrorStore';

import { previewTrack } from './fixtures';

const { __player } = jest.requireMock('react-native-track-player');

function numberedPreviewTrack(n: number): PlaybackTrack {
  return previewTrack({
    source: { kind: 'preview', previewUrl: `https://cdn.example/${n}.mp3` },
    title: `Track ${n}`,
  });
}

beforeEach(() => {
  useQueueStore.getState().clearQueue();
  usePlaybackErrorStore.getState().clear();
});

describe('createNativePlaybackActions', () => {
  it('play shows the track, clears the queue, and loads it natively', async () => {
    const setTrack = jest.fn();
    const { controls } = createNativePlaybackActions(setTrack);
    useQueueStore.getState().loadQueue([numberedPreviewTrack(1), numberedPreviewTrack(2)], 0, null);

    await controls.play(numberedPreviewTrack(3));

    expect(setTrack).toHaveBeenCalledWith(numberedPreviewTrack(3));
    expect(useQueueStore.getState().currentTrack()).toBeNull();
    expect(__player.calls('add')).toHaveLength(1);
    expect(__player.calls('play')).toHaveLength(1);
  });

  it('play reports a native load failure against the track', async () => {
    const { controls } = createNativePlaybackActions(jest.fn());
    __player.failNext('add', new Error('native add failed'));

    await controls.play(numberedPreviewTrack(1));

    expect(usePlaybackErrorStore.getState()).toMatchObject({
      key: trackKey(numberedPreviewTrack(1)),
      message: 'native add failed',
    });
  });

  it('seekTo resumes playback only when it was last synced as playing', async () => {
    const native = createNativePlaybackActions(jest.fn());

    native.controls.seekTo(1500);
    await new Promise(setImmediate);
    expect(__player.calls('seekTo')).toEqual([[1.5]]);
    expect(__player.calls('play')).toHaveLength(0);

    native.syncIsPlaying(true);
    native.controls.seekTo(3000);
    await new Promise(setImmediate);
    expect(__player.calls('play')).toHaveLength(1);
  });

  it('seekTo honours the initial playing state before the first sync', async () => {
    const native = createNativePlaybackActions(jest.fn(), true);

    native.controls.seekTo(2000);
    await new Promise(setImmediate);

    expect(__player.calls('play')).toHaveLength(1);
  });

  it('retry replays the remembered track when no queue is active', async () => {
    const native = createNativePlaybackActions(jest.fn());
    native.rememberTrack(numberedPreviewTrack(7));

    native.controls.retry();
    await new Promise(setImmediate);

    expect(__player.calls('add')[0]?.[0]).toMatchObject({ title: 'Track 7' });
  });

  it('skip commands swallow native rejections', async () => {
    const { controls } = createNativePlaybackActions(jest.fn());
    __player.failNext('skipToNext');

    await expect(controls.skipNext()).resolves.toBeUndefined();
  });

  it('stop resets the player and clears the displayed track', () => {
    const setTrack = jest.fn();
    const { controls } = createNativePlaybackActions(setTrack);

    controls.stop();

    expect(__player.calls('reset')).toHaveLength(1);
    expect(setTrack).toHaveBeenCalledWith(null);
  });
});
