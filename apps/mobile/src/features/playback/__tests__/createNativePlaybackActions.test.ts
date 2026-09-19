import { useQueueStore } from '@shared/playback/queueStore';
import { trackKey } from '@shared/playback/trackKey';
import type { PlaybackControls, PlaybackTrack } from '@shared/playback/types';

import {
  classifyNativeQueueFailure,
  createNativePlaybackActions,
  QUEUE_OUT_OF_SYNC_MESSAGE,
  QUEUE_UPDATE_FAILED_MESSAGE,
} from '../createNativePlaybackActions';
import { NativeQueueTimeoutError, withNativeQueue } from '../nativeQueueLock';
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

    it.each([
      ['removeQueueIndex', 'remove', (c: PlaybackControls) => c.removeQueueIndex(5)],
      ['skipToQueueIndex', 'skip', (c: PlaybackControls) => c.skipToQueueIndex(5)],
      ['skipPrevious', 'skipToPrevious', (c: PlaybackControls) => c.skipPrevious()],
    ] as const)(
      '%s classifies a stale-index rejection as permanent queue drift',
      async (op, nativeMethod, invoke) => {
        const { controls } = createNativePlaybackActions(jest.fn());
        useQueueStore.getState().loadQueue([numberedPreviewTrack(1)], 0, null);
        __player.failNext(
          nativeMethod,
          nativeError('index_out_of_bounds', 'The index is out of bounds'),
        );

        await expect(invoke(controls)).resolves.toBeUndefined();

        expect(usePlaybackErrorStore.getState()).toMatchObject({
          key: trackKey(numberedPreviewTrack(1)),
          kind: 'queue_out_of_sync',
          message: QUEUE_OUT_OF_SYNC_MESSAGE,
        });
        expect(warn).toHaveBeenCalledWith(
          '[playback] native queue mutation failed',
          expect.objectContaining({ op, kind: 'permanent', code: 'index_out_of_bounds' }),
        );
      },
    );

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
