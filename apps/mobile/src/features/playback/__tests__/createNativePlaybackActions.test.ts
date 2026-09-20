import TrackPlayer from 'react-native-track-player';

import { useQueueStore } from '@shared/playback/queueStore';
import { type TrackKey, trackKey } from '@shared/playback/trackKey';
import type { PlaybackTrack } from '@shared/playback/types';

import {
  classifyNativeQueueFailure,
  createNativePlaybackActions,
  QUEUE_OUT_OF_SYNC_MESSAGE,
  QUEUE_UPDATE_FAILED_MESSAGE,
} from '../createNativePlaybackActions';
import { NativeQueueTimeoutError, withNativeQueue } from '../nativeQueueLock';
import { usePlaybackErrorStore } from '../playbackErrorStore';
import { reportingQueueFailure } from '../queueFailureReport';

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
      kind: 'unknown',
      message: 'native add failed',
    });
  });

  it('play reports a refused stream load with the auth kind', async () => {
    const { controls } = createNativePlaybackActions(jest.fn());
    __player.failNext(
      'add',
      Object.assign(new Error('Response code: 403'), { code: 'android-io-bad-http-status' }),
    );

    await controls.play(numberedPreviewTrack(1));

    expect(usePlaybackErrorStore.getState().kind).toBe('auth');
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

  describe('queue mutation failures', () => {
    let warn: jest.SpyInstance;

    beforeEach(() => {
      warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
    });

    afterEach(() => {
      warn.mockRestore();
    });

    it('skip commands never reject to the UI handler', async () => {
      const { controls } = createNativePlaybackActions(jest.fn());
      __player.failNext('skipToNext');

      await expect(controls.skipNext()).resolves.toBeUndefined();
    });

    function nativeError(code: string, message: string): Error {
      return Object.assign(new Error(message), { code });
    }

    it('reports a rejected skip against the current queue track instead of swallowing it', async () => {
      const { controls } = createNativePlaybackActions(jest.fn());
      useQueueStore
        .getState()
        .loadQueue([numberedPreviewTrack(1), numberedPreviewTrack(2)], 0, null);
      __player.failNext('skipToNext', new Error('bridge hiccup'));

      await controls.skipNext();

      expect(usePlaybackErrorStore.getState()).toMatchObject({
        key: trackKey(numberedPreviewTrack(1)),
        kind: 'queue_update_failed',
        message: QUEUE_UPDATE_FAILED_MESSAGE,
      });
      expect(warn).toHaveBeenCalledWith(
        '[playback] native queue mutation failed',
        expect.objectContaining({ op: 'skipNext', kind: 'transient' }),
      );
    });

    it('skipPrevious classifies a stale-index rejection as permanent queue drift', async () => {
      const { controls } = createNativePlaybackActions(jest.fn());
      useQueueStore.getState().loadQueue([numberedPreviewTrack(1)], 0, null);
      __player.failNext(
        'skipToPrevious',
        nativeError('index_out_of_bounds', 'The index is out of bounds'),
      );

      await expect(controls.skipPrevious()).resolves.toBeUndefined();

      expect(usePlaybackErrorStore.getState()).toMatchObject({
        key: trackKey(numberedPreviewTrack(1)),
        kind: 'queue_out_of_sync',
        message: QUEUE_OUT_OF_SYNC_MESSAGE,
      });
      expect(warn).toHaveBeenCalledWith(
        '[playback] native queue mutation failed',
        expect.objectContaining({
          op: 'skipPrevious',
          kind: 'permanent',
          code: 'index_out_of_bounds',
        }),
      );
    });

    // The index-carrying commands no longer read an out-of-bounds rejection as drift:
    // since #1732 native holds a window of the queue, so a position past its end is the
    // normal case on a long queue.
    it('removeQueueIndex reports nothing when the position is past the native window', async () => {
      const { controls } = createNativePlaybackActions(jest.fn());
      useQueueStore.getState().loadQueue([numberedPreviewTrack(1)], 0, null);
      __player.failNext('remove', nativeError('index_out_of_bounds', 'The index is out of bounds'));

      await controls.removeQueueIndex(500);

      expect(usePlaybackErrorStore.getState().key).toBeNull();
    });

    it('skipToQueueIndex rebuilds the native queue at a target past the native window', async () => {
      const { controls } = createNativePlaybackActions(jest.fn());
      useQueueStore
        .getState()
        .loadQueue([numberedPreviewTrack(1), numberedPreviewTrack(2)], 0, null);
      __player.failNext('skip', nativeError('index_out_of_bounds', 'The index is out of bounds'));

      await controls.skipToQueueIndex(1);

      expect(usePlaybackErrorStore.getState().key).toBeNull();
      expect(__player.calls('add')[0]?.[0]).toMatchObject([
        { title: 'Track 1' },
        { title: 'Track 2' },
      ]);
    });

    it('reports a failed append against the remembered track when no queue is active', async () => {
      const native = createNativePlaybackActions(jest.fn());
      native.rememberTrack(numberedPreviewTrack(9));
      __player.failNext('add', nativeError('player_not_initialized', 'not initialized'));

      await native.controls.appendToQueue(numberedPreviewTrack(10));

      expect(usePlaybackErrorStore.getState()).toMatchObject({
        key: trackKey(numberedPreviewTrack(9)),
        kind: 'queue_out_of_sync',
        message: QUEUE_OUT_OF_SYNC_MESSAGE,
      });
    });

    it('classifies a native queue timeout as transient', () => {
      expect(classifyNativeQueueFailure(new NativeQueueTimeoutError(15_000))).toBe('transient');
      expect(classifyNativeQueueFailure(nativeError('no_current_item', 'none'))).toBe('permanent');
      expect(classifyNativeQueueFailure('not an error')).toBe('transient');
    });

    it('does not pin a stale failure on a track loaded after the op was issued', async () => {
      const { controls } = createNativePlaybackActions(jest.fn());
      useQueueStore.getState().loadQueue([numberedPreviewTrack(1)], 0, null);
      __player.failNext('skipToNext');

      const pending = controls.skipNext();
      useQueueStore.getState().loadQueue([numberedPreviewTrack(2)], 0, null);
      await pending;

      expect(usePlaybackErrorStore.getState().key).toBeNull();
      expect(warn).toHaveBeenCalledTimes(1);
    });

    it('logs but reports nothing when no track is displayed', async () => {
      const { controls } = createNativePlaybackActions(jest.fn());
      __player.failNext('skipToNext');

      await controls.skipNext();

      expect(usePlaybackErrorStore.getState().key).toBeNull();
      expect(warn).toHaveBeenCalledTimes(1);
    });
  });

  // #1730: pause/resume/seekTo used to be bare `void TrackPlayer.x()` calls, so a native
  // rejection became an unhandled rejection nobody saw, and an unserialized seek could
  // land after a seek the user made later.
  describe('transport commands', () => {
    let warn: jest.SpyInstance;

    beforeEach(() => {
      warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
    });

    afterEach(() => {
      warn.mockRestore();
    });

    it('logs a rejected pause rather than leaving the rejection unhandled', async () => {
      const { controls } = createNativePlaybackActions(jest.fn());
      __player.failNext('pause', new Error('no current item'));

      controls.pause();
      await new Promise(setImmediate);

      expect(warn).toHaveBeenCalledWith(
        '[playback] native command failed',
        expect.objectContaining({ message: 'no current item' }),
      );
    });

    it('logs a rejected resume rather than leaving the rejection unhandled', async () => {
      const { controls } = createNativePlaybackActions(jest.fn());
      __player.failNext('play', new Error('player not ready'));

      controls.resume();
      await new Promise(setImmediate);

      expect(warn).toHaveBeenCalledWith(
        '[playback] native command failed',
        expect.objectContaining({ message: 'player not ready' }),
      );
    });

    it('reports a rejected seek against the current queue track', async () => {
      const { controls } = createNativePlaybackActions(jest.fn());
      useQueueStore.getState().loadQueue([numberedPreviewTrack(1)], 0, null);
      __player.failNext(
        'seekTo',
        Object.assign(new Error('no current item'), { code: 'no_current_item' }),
      );

      controls.seekTo(1500);
      await new Promise(setImmediate);

      expect(usePlaybackErrorStore.getState()).toMatchObject({
        key: trackKey(numberedPreviewTrack(1)),
        kind: 'queue_out_of_sync',
        message: QUEUE_OUT_OF_SYNC_MESSAGE,
      });
      expect(warn).toHaveBeenCalledWith(
        '[playback] native queue mutation failed',
        expect.objectContaining({ op: 'seekTo', kind: 'permanent' }),
      );
    });

    it('holds a second seek until the first has settled natively', async () => {
      const native = createNativePlaybackActions(jest.fn());
      let releaseFirstSeek = (): void => undefined;
      (TrackPlayer.seekTo as jest.Mock).mockImplementationOnce(
        () =>
          new Promise<void>((resolve) => {
            releaseFirstSeek = resolve;
          }),
      );

      native.controls.seekTo(1000);
      native.controls.seekTo(2000);
      await new Promise(setImmediate);

      expect(__player.calls('seekTo')).toEqual([[1]]);

      releaseFirstSeek();
      await new Promise(setImmediate);

      expect(__player.calls('seekTo')).toEqual([[1], [2]]);
    });
  });

  describe('stop', () => {
    it('resets the player and clears the displayed track', async () => {
      const setTrack = jest.fn();
      const { controls } = createNativePlaybackActions(setTrack);

      controls.stop();
      await new Promise(setImmediate);

      expect(__player.calls('reset')).toHaveLength(1);
      expect(setTrack).toHaveBeenCalledWith(null);
    });

    // #1724: an unlocked reset used to run while loadNativeQueue was still resolving
    // URLs, so the load went on to refill the queue the user had just emptied.
    it('leaves the native queue empty when a queue load is still mid-flight', async () => {
      const { controls } = createNativePlaybackActions(jest.fn());

      const loading = controls.startQueue([numberedPreviewTrack(1), numberedPreviewTrack(2)], 0);
      controls.stop();
      await loading;
      await new Promise(setImmediate);

      expect(__player.calls('add')).toHaveLength(0);
      expect(__player.calls('play')).toHaveLength(0);
      expect(__player.calls('reset')).toHaveLength(1);
    });

    it('waits for an in-flight native queue op instead of resetting underneath it', async () => {
      const { controls } = createNativePlaybackActions(jest.fn());
      let releaseHeldOp = (): void => undefined;
      const heldOp = withNativeQueue(
        () =>
          new Promise<void>((resolve) => {
            releaseHeldOp = resolve;
          }),
      );

      controls.stop();
      await new Promise(setImmediate);
      expect(__player.calls('reset')).toHaveLength(0);

      releaseHeldOp();
      await heldOp;
      await new Promise(setImmediate);

      expect(__player.calls('reset')).toHaveLength(1);
    });
  });
});

describe('reportingQueueFailure — the still-current policy lives once', () => {
  let warn: jest.SpyInstance;

  beforeEach(() => {
    warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
  });

  afterEach(() => {
    warn.mockRestore();
  });

  it('reports the rejection against the key its getter returns at rejection time', async () => {
    const key = trackKey(numberedPreviewTrack(1));

    await reportingQueueFailure(
      () => key,
      'probe',
      () => Promise.reject(new Error('boom')),
    );

    expect(usePlaybackErrorStore.getState().key).toBe(key);
  });

  it('reports nothing when the getter key changed between the call and the rejection', async () => {
    let key: TrackKey | null = trackKey(numberedPreviewTrack(1));

    await reportingQueueFailure(
      () => key,
      'probe',
      () => {
        key = trackKey(numberedPreviewTrack(2));
        return Promise.reject(new Error('boom'));
      },
    );

    expect(usePlaybackErrorStore.getState().key).toBeNull();
  });
});
