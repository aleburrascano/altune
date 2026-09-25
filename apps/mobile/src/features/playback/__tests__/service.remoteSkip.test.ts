// Regression for issue #1742: a skip from the lock screen, a car or a headset that the
// native queue rejects used to be swallowed — a dead button, and nothing in the client
// logs to explain it. It is now classified, logged and surfaced like an in-app skip.

import TrackPlayer, { Event } from 'react-native-track-player';

import { RESTART_THRESHOLD_MS } from '@shared/playback/constants';
import { useQueueStore } from '@shared/playback/queueStore';
import { trackKey } from '@shared/playback/trackKey';
import type { PlaybackTrack } from '@shared/playback/types';
import { setSignedInUser } from '@shared/session/signOutCleanup';

import {
  QUEUE_OUT_OF_SYNC_MESSAGE,
  QUEUE_UPDATE_FAILED_MESSAGE,
} from '../createNativePlaybackActions';
import { usePlaybackErrorStore } from '../playbackErrorStore';
import { playbackService } from '../service';

import { previewTrack } from './fixtures';

const { __player } = jest.requireMock('react-native-track-player');
const player = TrackPlayer as unknown as { getProgress: jest.Mock };

const RESTART_THRESHOLD_SECONDS = RESTART_THRESHOLD_MS / 1000;

function numberedPreviewTrack(n: number): PlaybackTrack {
  return previewTrack({
    source: { kind: 'preview', previewUrl: `https://cdn.example/${n}.mp3` },
    title: `Track ${n}`,
  });
}

function nativeError(code: string, message: string): Error {
  return Object.assign(new Error(message), { code });
}

async function remoteHandler(event: unknown): Promise<() => void> {
  await playbackService();
  const registration = __player
    .calls('addEventListener')
    .find(([registered]: [unknown]) => registered === event);
  if (!registration) throw new Error(`no ${String(event)} listener was registered`);
  return registration[1];
}

function settled(): Promise<void> {
  return new Promise((resolve) => setImmediate(resolve));
}

let warn: jest.SpyInstance;

// Remote commands that move audio are no-ops without a signed-in user (#827).
beforeEach(() => {
  setSignedInUser(true);
  player.getProgress.mockResolvedValue({ position: 0, duration: 200, buffered: 0 });
  warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
});

afterEach(() => {
  jest.restoreAllMocks();
  usePlaybackErrorStore.getState().clear();
  useQueueStore.getState().clearQueue();
});

describe('playbackService — a rejected remote skip is reported, not swallowed (#1742)', () => {
  it('surfaces a bridge failure on the current track when a hardware next rejects', async () => {
    useQueueStore
      .getState()
      .loadQueue([numberedPreviewTrack(1), numberedPreviewTrack(2)], 0, null);
    const skipNext = await remoteHandler(Event.RemoteNext);
    __player.failNext('skipToNext', new Error('bridge hiccup'));

    skipNext();
    await settled();

    expect(usePlaybackErrorStore.getState()).toMatchObject({
      key: trackKey(numberedPreviewTrack(1)),
      kind: 'queue_update_failed',
      message: QUEUE_UPDATE_FAILED_MESSAGE,
    });
    expect(warn).toHaveBeenCalledWith(
      '[playback] native queue mutation failed',
      expect.objectContaining({ op: 'remoteSkipNext', kind: 'transient' }),
    );
  });

  it('classifies a stale-index rejection from a hardware previous as permanent drift', async () => {
    useQueueStore.getState().loadQueue([numberedPreviewTrack(1)], 0, null);
    const skipPrevious = await remoteHandler(Event.RemotePrevious);
    __player.failNext(
      'skipToPrevious',
      nativeError('index_out_of_bounds', 'The index is out of bounds'),
    );

    skipPrevious();
    await settled();

    expect(usePlaybackErrorStore.getState()).toMatchObject({
      key: trackKey(numberedPreviewTrack(1)),
      kind: 'queue_out_of_sync',
      message: QUEUE_OUT_OF_SYNC_MESSAGE,
    });
    expect(warn).toHaveBeenCalledWith(
      '[playback] native queue mutation failed',
      expect.objectContaining({
        op: 'remoteSkipPrevious',
        kind: 'permanent',
        code: 'index_out_of_bounds',
      }),
    );
  });

  it('reports a restart seek the player rejects past the threshold', async () => {
    player.getProgress.mockResolvedValue({
      position: RESTART_THRESHOLD_SECONDS + 1,
      duration: 200,
      buffered: 0,
    });
    useQueueStore.getState().loadQueue([numberedPreviewTrack(1)], 0, null);
    const skipPrevious = await remoteHandler(Event.RemotePrevious);
    __player.failNext('seekTo', new Error('seek refused'));

    skipPrevious();
    await settled();

    expect(usePlaybackErrorStore.getState().key).toBe(trackKey(numberedPreviewTrack(1)));
    expect(warn).toHaveBeenCalledWith(
      '[playback] native queue mutation failed',
      expect.objectContaining({ op: 'remoteSkipPrevious', kind: 'transient' }),
    );
  });

  it('still restarts the current track rather than stepping back past the threshold', async () => {
    player.getProgress.mockResolvedValue({
      position: RESTART_THRESHOLD_SECONDS + 1,
      duration: 200,
      buffered: 0,
    });
    const skipPrevious = await remoteHandler(Event.RemotePrevious);

    skipPrevious();
    await settled();

    expect(__player.calls('seekTo')).toEqual([[0]]);
    expect(__player.calls('skipToPrevious')).toHaveLength(0);
    expect(warn).not.toHaveBeenCalled();
  });
});
