import * as FileSystem from 'expo-file-system';
import TrackPlayer, { Event, type RemoteDuckEvent } from 'react-native-track-player';

import {
  _resetAudioCacheInvalidatorsForTest,
  invalidateAudioCaches,
} from '@shared/acquisition/audioCacheInvalidation';
import { asTrackId, type TrackId } from '@shared/api-client/ids';
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
import {
  MAX_TRACKED_RECOVERIES,
  RECOVERY_ATTEMPTS_PER_TRACK,
  RECOVERY_COOLDOWN_BASE_MS,
  playbackService,
  resetPlaybackForSignOut,
} from '../service';

import { libraryTrack, previewTrack } from './fixtures';

const { __player } = jest.requireMock('react-native-track-player');
const { __http } = require('../../../../jest/doubles/fetch.js');

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: {
    auth: {
      getSession: jest
        .fn()
        .mockResolvedValue({ data: { session: { access_token: 'tok' } }, error: null }),
    },
  },
}));

// The invalidator registry is typed in `TrackId`, but a cast can still smuggle a raw string
// through it, so the service re-parses before the prefetch cache sees an id. These pin both
// halves of that seam.
describe('the audio cache invalidator the service registers', () => {
  const { __fs } = FileSystem as unknown as {
    __fs: { seedFile(uri: string, contents: string): void; allFiles(): Record<string, string> };
  };

  const CACHE_DIR_URI = 'file:///cache/audio-prefetch';

  function cachedNames(): string[] {
    return Object.keys(__fs.allFiles())
      .map((uri) => uri.slice(CACHE_DIR_URI.length + 1))
      .sort();
  }

  beforeEach(async () => {
    _resetAudioCacheInvalidatorsForTest();
    await playbackService();
  });

  describe('playbackService — the registered audio cache invalidator', () => {
    it('evicts every cached file of a track whose id has the branded shape', () => {
      __fs.seedFile(`${CACHE_DIR_URI}/t1.v1.mp3`, 'a');
      __fs.seedFile(`${CACHE_DIR_URI}/t1.v2.mp3`, 'b');
      __fs.seedFile(`${CACHE_DIR_URI}/t2.v1.mp3`, 'c');

      invalidateAudioCaches(asTrackId('t1'));

      expect(cachedNames()).toEqual(['t2.v1.mp3']);
    });

    it('leaves the cache untouched for an id the brand rejects', () => {
      __fs.seedFile(`${CACHE_DIR_URI}/t1.v1.mp3`, 'a');

      invalidateAudioCaches('../../t1' as TrackId);

      expect(cachedNames()).toEqual(['t1.v1.mp3']);
    });
  });
});

describe('native PlaybackError handling', () => {
  type PlaybackErrorHandler = (data: { code: string; message: string }) => void;

  async function playbackErrorHandler(): Promise<PlaybackErrorHandler> {
    await playbackService();
    const registration = __player
      .calls('addEventListener')
      .find(([event]: [unknown]) => event === Event.PlaybackError);
    if (!registration) throw new Error('no PlaybackError listener was registered');
    return registration[1];
  }

  afterEach(() => {
    usePlaybackErrorStore.getState().clear();
    useQueueStore.getState().clearQueue();
  });

  describe('playbackService — a native PlaybackError never stores a signed URL or token', () => {
    it('reports the failing track with the tokenized stream URL redacted', async () => {
      const track = previewTrack();
      useQueueStore.getState().loadQueue([track], 0, null);
      const handler = await playbackErrorHandler();
      const streamUrl =
        'https://audio.altune.example/stream/trk-1?X-Amz-Signature=deadbeef&token=s3cr3t-token';

      handler({
        code: 'android-io-bad-http-status',
        message: `Response code: 403 url=${streamUrl}`,
      });

      await new Promise((resolve) => setImmediate(resolve));
      const stored = usePlaybackErrorStore.getState();
      expect(stored.key).toBe(trackKey(track));
      expect(stored.message).not.toMatch(/s3cr3t-token|deadbeef|audio\.altune\.example/);
      expect(stored.message).toBe('Response code: 403 url=[redacted url]');
    });
  });

  describe('playbackService — a native PlaybackError is stored with a typed failure kind', () => {
    async function kindFor(code: string, message: string): Promise<unknown> {
      const track = previewTrack();
      useQueueStore.getState().loadQueue([track], 0, null);
      const handler = await playbackErrorHandler();

      handler({ code, message });

      await new Promise((resolve) => setImmediate(resolve));
      expect(usePlaybackErrorStore.getState().key).toBe(trackKey(track));
      return usePlaybackErrorStore.getState().kind;
    }

    it('tells a lost connection apart from an undecodable file', async () => {
      const network = await kindFor('android-io-network-connection-failed', 'Source error');
      const decode = await kindFor('android-decoding-failed', 'Source error');

      expect(network).toBe('network');
      expect(decode).toBe('decode');
    });

    it('classifies a refused (403) stream request as auth and a 404 as not found', async () => {
      expect(await kindFor('android-io-bad-http-status', 'Response code: 403')).toBe('auth');
      expect(await kindFor('android-io-bad-http-status', 'Response code: 404')).toBe('not_found');
    });

    it('stores unknown when the native event carries no code or message', async () => {
      const kind = await kindFor(undefined as unknown as string, undefined as unknown as string);

      expect(kind).toBe('unknown');
      expect(usePlaybackErrorStore.getState().message).toBe('Playback failed');
    });
  });

  // Regression for issue #1745: `handlePlaybackError` re-hit the recover endpoint on every single
  // native PlaybackError, so a fleet-wide fault (a batch of bad signed URLs, an OS codec
  // regression) amplified itself against the very endpoint already in trouble.
  describe('playbackService — recovery is budgeted per track', () => {
    const RECOVER_TRACK_1 = 'POST /v1/tracks/trk-1/audio/recover';
    const RECOVER_TRACK_2 = 'POST /v1/tracks/trk-2/audio/recover';
    const MANY_ERRORS = RECOVERY_ATTEMPTS_PER_TRACK + 6;

    let now = 0;

    function libraryTrackWithId(trackId: string): PlaybackTrack {
      return libraryTrack({ source: { kind: 'library', trackId: asTrackId(trackId) } });
    }

    async function failPlaybackOnce(onError: PlaybackErrorHandler): Promise<void> {
      onError({ code: 'android-io-bad-http-status', message: 'Response code: 403' });
      await new Promise((resolve) => setImmediate(resolve));
    }

    async function failPlayback(times: number): Promise<PlaybackErrorHandler> {
      const onError = await playbackErrorHandler();
      for (let i = 0; i < times; i += 1) await failPlaybackOnce(onError);
      return onError;
    }

    beforeEach(() => {
      now = Date.parse('2026-09-19T10:00:00Z');
      jest.spyOn(Date, 'now').mockImplementation(() => now);
      __http.replyAll({ status: 204 });
      useQueueStore.getState().loadQueue([libraryTrackWithId('trk-1')], 0, null);
    });

    afterEach(async () => {
      jest.restoreAllMocks();
      await resetPlaybackForSignOut();
    });

    it('asks the server to recover the track the first time it fails', async () => {
      await failPlayback(1);

      expect(__http.countFor(RECOVER_TRACK_1)).toBe(1);
    });

    it('stops asking once the track has spent its budget, however many errors arrive', async () => {
      await failPlayback(MANY_ERRORS);

      expect(__http.countFor(RECOVER_TRACK_1)).toBe(RECOVERY_ATTEMPTS_PER_TRACK);
    });

    it('still reports every failure to the user once the budget is spent', async () => {
      const onError = await failPlayback(MANY_ERRORS);

      onError({ code: 'android-decoding-failed', message: 'Source error' });

      await new Promise((resolve) => setImmediate(resolve));
      expect(usePlaybackErrorStore.getState().kind).toBe('decode');
    });

    it('keeps refusing while the track is still cooling down', async () => {
      const onError = await failPlayback(RECOVERY_ATTEMPTS_PER_TRACK);

      now += RECOVERY_COOLDOWN_BASE_MS - 1;
      await failPlaybackOnce(onError);

      expect(__http.countFor(RECOVER_TRACK_1)).toBe(RECOVERY_ATTEMPTS_PER_TRACK);
    });

    it('gives the track one more attempt once its cooldown has passed', async () => {
      const onError = await failPlayback(RECOVERY_ATTEMPTS_PER_TRACK);

      now += RECOVERY_COOLDOWN_BASE_MS;
      await failPlaybackOnce(onError);

      expect(__http.countFor(RECOVER_TRACK_1)).toBe(RECOVERY_ATTEMPTS_PER_TRACK + 1);
    });

    it('doubles the cooldown each time a track spends another attempt', async () => {
      const onError = await failPlayback(RECOVERY_ATTEMPTS_PER_TRACK);
      now += RECOVERY_COOLDOWN_BASE_MS;
      await failPlaybackOnce(onError);

      now += RECOVERY_COOLDOWN_BASE_MS;
      await failPlaybackOnce(onError);

      expect(__http.countFor(RECOVER_TRACK_1)).toBe(RECOVERY_ATTEMPTS_PER_TRACK + 1);
    });

    it('holds a budget per track, so one failing track cannot silence another', async () => {
      await failPlayback(MANY_ERRORS);

      useQueueStore.getState().loadQueue([libraryTrackWithId('trk-2')], 0, null);
      await failPlayback(1);

      expect(__http.countFor(RECOVER_TRACK_2)).toBe(1);
    });

    it('refuses a new track once it is already tracking a queue-full of failing ones', async () => {
      const onError = await playbackErrorHandler();
      for (let i = 0; i < MAX_TRACKED_RECOVERIES; i += 1) {
        useQueueStore.getState().loadQueue([libraryTrackWithId(`trk-f${i}`)], 0, null);
        await failPlaybackOnce(onError);
      }

      useQueueStore.getState().loadQueue([libraryTrackWithId('trk-2')], 0, null);
      await failPlaybackOnce(onError);

      expect(__http.countFor(RECOVER_TRACK_2)).toBe(0);
    });

    it('gives the next user a fresh budget after a sign-out', async () => {
      await failPlayback(MANY_ERRORS);

      await resetPlaybackForSignOut();
      useQueueStore.getState().loadQueue([libraryTrackWithId('trk-1')], 0, null);
      await failPlayback(1);

      expect(__http.countFor(RECOVER_TRACK_1)).toBe(RECOVERY_ATTEMPTS_PER_TRACK + 1);
    });
  });
});

describe('RemoteDuck interruption handling', () => {
  async function remoteDuckHandler(): Promise<(data: RemoteDuckEvent) => void> {
    await playbackService();
    const registration = __player
      .calls('addEventListener')
      .find(([event]: [unknown]) => event === Event.RemoteDuck);
    if (!registration) throw new Error('no RemoteDuck listener was registered');
    return registration[1];
  }

  describe('playbackService — RemoteDuck resumes playback after an interruption', () => {
    // Resuming needs a signed-in user (#827); these cases model an active session.
    beforeEach(() => {
      setSignedInUser(true);
    });

    it('does not resume when no user is signed in (#827)', async () => {
      const handler = await remoteDuckHandler();
      setSignedInUser(false);

      handler({ paused: false, permanent: false });

      expect(__player.calls('play')).toHaveLength(0);
    });

    it('registers a RemoteDuck listener', async () => {
      const handler = await remoteDuckHandler();

      expect(typeof handler).toBe('function');
    });

    it('resumes playback when a non-permanent interruption ends', async () => {
      const handler = await remoteDuckHandler();

      handler({ paused: false, permanent: false });

      expect(__player.calls('play')).toHaveLength(1);
    });

    it('pauses when an interruption begins', async () => {
      const handler = await remoteDuckHandler();

      handler({ paused: true, permanent: false });

      expect(__player.calls('pause')).toHaveLength(1);
      expect(__player.calls('play')).toHaveLength(0);
    });

    it('stays paused on a permanent focus loss', async () => {
      const handler = await remoteDuckHandler();

      handler({ paused: true, permanent: true });

      expect(__player.calls('play')).toHaveLength(0);
      expect(__player.calls('pause')).toHaveLength(0);
    });

    it('does not fight autoHandleInterruptions: it only calls play on resume, never pause', async () => {
      const handler = await remoteDuckHandler();

      handler({ paused: false, permanent: false });

      expect(__player.calls('play')).toHaveLength(1);
      expect(__player.calls('pause')).toHaveLength(0);
    });
  });
});

// Regression for issue #1742: a skip from the lock screen, a car or a headset that the
// native queue rejects used to be swallowed — a dead button, and nothing in the client
// logs to explain it. It is now classified, logged and surfaced like an in-app skip.
describe('remote skip commands', () => {
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
});
