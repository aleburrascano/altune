import { Event } from 'react-native-track-player';

import { asTrackId } from '@shared/api-client/ids';
import { useQueueStore } from '@shared/playback/queueStore';
import { trackKey } from '@shared/playback/trackKey';
import type { PlaybackTrack } from '@shared/playback/types';

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

    handler({ code: 'android-io-bad-http-status', message: `Response code: 403 url=${streamUrl}` });

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
