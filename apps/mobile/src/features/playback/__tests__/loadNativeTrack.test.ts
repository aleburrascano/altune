import TrackPlayer, { Event } from 'react-native-track-player';

import { fetchAudioUrls, type ResolvedAudioUrl } from '@shared/api-client/audio';
import { asTrackId } from '@shared/api-client/ids';
import { ApiError } from '@shared/errors';
import { usePinnedStore } from '@shared/offline/pinnedStore';
import { orderedQueueTracks, useQueueStore } from '@shared/playback/queueStore';
import { trackKey } from '@shared/playback/trackKey';
import type { PlaybackTrack } from '@shared/playback/types';

import {
  appendNativeTrack,
  insertNativeTrackNext,
  loadNativeQueue,
  loadNativeTrack,
  refreshUpcomingPresign,
  reorderUpcomingNative,
} from '../native/loadNativeTrack';
import { claimLoad } from '../loadToken';
import { NATIVE_QUEUE_OP_TIMEOUT_MS, withNativeQueue } from '../native/nativeQueueLock';
import { forgetAllSwaps } from '../native/nativeTrackSwap';
import { usePlaybackErrorStore } from '../playbackErrorStore';
import { NATIVE_QUEUE_WINDOW } from '../presignWindow';
import { playbackService, resetPlaybackForSignOut } from '../native/service';

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

jest.mock('@shared/api-client/audio', () => {
  const actual = jest.requireActual('@shared/api-client/audio');
  return { ...actual, fetchAudioUrls: jest.fn() };
});

const fetchUrls = fetchAudioUrls as jest.MockedFunction<typeof fetchAudioUrls>;
const realFetchAudioUrls = jest.requireActual<{ fetchAudioUrls: typeof fetchAudioUrls }>(
  '@shared/api-client/audio',
).fetchAudioUrls;

const nativePlayer = TrackPlayer as unknown as Record<string, jest.Mock>;
const STUBBED_PLAYER_METHODS = [
  'add',
  'getActiveTrack',
  'getActiveTrackIndex',
  'getQueue',
  'load',
  'remove',
  'removeUpcomingTracks',
  'reset',
  'seekTo',
  'skip',
] as const;
const defaultPlayerImpls = new Map(
  STUBBED_PLAYER_METHODS.map((name) => [name, nativePlayer[name]!.getMockImplementation()]),
);

function restorePlayerDefault(name: (typeof STUBBED_PLAYER_METHODS)[number]): void {
  nativePlayer[name]!.mockReset().mockImplementation(defaultPlayerImpls.get(name));
}

beforeEach(() => {
  for (const name of STUBBED_PLAYER_METHODS) restorePlayerDefault(name);
  fetchUrls.mockImplementation(realFetchAudioUrls);
});

describe('loadNativeQueue rollback of a failed add', () => {
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
      (TrackPlayer.add as jest.Mock).mockImplementationOnce(async () => {
        __player.failNext('reset', new Error('reset failed'));
        throw addError;
      });

      await expect(loadNativeQueue(makeTracks(3), 0, { autoplay: false })).rejects.toBe(addError);
      expect(lastCallOrder(TrackPlayer.reset)).toBeGreaterThan(lastCallOrder(TrackPlayer.add));
    });

    it('skips the rollback when a newer load superseded it before the add rejected', async () => {
      const addError = new Error('late add rejection');
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
});

describe('native ops superseded by a newer load', () => {
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
});

describe('presign failure and pinned audio', () => {
  const PINNED_URI = 'file:///document/offline-audio/t1.mp3';
  const TRACK = libraryTrack({ source: { kind: 'library', trackId: asTrackId('t1') } });

  function pinnedVersion(trackId: string): string | undefined {
    const entry = usePinnedStore.getState().entries[trackId];
    return entry?.status === 'ready' ? entry.version : undefined;
  }

  function addedUrls(): string[] {
    return (__player.calls('add') as unknown[][]).flatMap(([arg]) =>
      (Array.isArray(arg) ? arg : [arg]).map((t: { url: string }) => t.url),
    );
  }

  beforeEach(() => {
    jest.spyOn(console, 'warn').mockImplementation(() => undefined);
    usePinnedStore.setState({
      entries: {
        t1: { trackId: asTrackId('t1'), status: 'ready', uri: PINNED_URI, version: 'v1' },
      },
      queue: [],
      isWorking: false,
    });
  });

  describe('presign failure and pinned audio (#828)', () => {
    it.each([401, 403])(
      'does not serve the pinned file when presign is denied with %i',
      async (status) => {
        __http.reply('POST /v1/audio-urls', { status, json: { code: 'forbidden' } });

        await loadNativeTrack(TRACK, { autoplay: false });

        expect(addedUrls()).toHaveLength(1);
        expect(addedUrls()).not.toContain(PINNED_URI);
        expect(addedUrls()[0]).toMatch(/\/v1\/tracks\/t1\/audio$/);
      },
    );

    it('does not serve the pinned file to a queue load or append when denied', async () => {
      __http.reply('POST /v1/audio-urls', { status: 403 });

      await loadNativeQueue([TRACK], 0, { autoplay: false });
      await appendNativeTrack(TRACK);

      expect(addedUrls()).toHaveLength(2);
      expect(addedUrls()).not.toContain(PINNED_URI);
    });

    it('still serves the pinned file when presign fails because the device is offline', async () => {
      __http.fail('POST /v1/audio-urls');

      await loadNativeTrack(TRACK, { autoplay: false });

      expect(addedUrls()).toEqual([PINNED_URI]);
    });

    it('still serves the pinned file when the server faults rather than denies', async () => {
      __http.reply('POST /v1/audio-urls', { status: 503 });

      await loadNativeTrack(TRACK, { autoplay: false });

      expect(addedUrls()).toEqual([PINNED_URI]);
    });

    it('serves the pinned file when presign succeeds with a matching version', async () => {
      __http.reply('POST /v1/audio-urls', {
        json: { urls: [{ track_id: 't1', url: 'https://signed.example/t1', version: 'v1' }] },
      });

      await loadNativeTrack(TRACK, { autoplay: false });

      expect(addedUrls()).toEqual([PINNED_URI]);
    });

    it('streams instead of the pinned file, and drops the stale copy, when the server has moved on a version', async () => {
      __http.reply('POST /v1/audio-urls', {
        json: { urls: [{ track_id: 't1', url: 'https://signed.example/t1', version: 'v2' }] },
      });

      await loadNativeTrack(TRACK, { autoplay: false });

      expect(addedUrls()).toEqual(['https://signed.example/t1']);
      expect(pinnedVersion('t1')).not.toBe('v1');
    });
  });
});

describe('native queue ops superseded by a sign-out or queue switch', () => {
  const A_TRACK = libraryTrack({ source: { kind: 'library', trackId: asTrackId('trk-of-a') } });
  const B_TRACK = libraryTrack({ source: { kind: 'library', trackId: asTrackId('trk-of-b') } });

  function nativeAddedIds(): string[] {
    const addCalls = __player.calls('add') as [{ id: string } | { id: string }[]][];
    return addCalls.flatMap(([added]) => (Array.isArray(added) ? added : [added])).map((t) => t.id);
  }

  describe('native queue ops in flight at sign-out (#827)', () => {
    it('an append started before sign-out does not add the track after the reset', async () => {
      const append = appendNativeTrack(A_TRACK);
      await resetPlaybackForSignOut();
      await append;

      expect(__player.calls('reset')).toHaveLength(1);
      expect(__player.calls('add')).toHaveLength(0);
    });

    it('an insert-next started before sign-out does not add the track after the reset', async () => {
      const insert = insertNativeTrackNext(A_TRACK, 1);
      await resetPlaybackForSignOut();
      await insert;

      expect(__player.calls('add')).toHaveLength(0);
    });

    it('an upcoming reorder started before sign-out leaves the reset queue alone', async () => {
      const reorder = reorderUpcomingNative([A_TRACK]);
      await resetPlaybackForSignOut();
      await reorder;

      expect(__player.calls('removeUpcomingTracks')).toHaveLength(0);
      expect(__player.calls('add')).toHaveLength(0);
    });

    it('an append started after sign-out still reaches the native queue', async () => {
      await resetPlaybackForSignOut();
      await appendNativeTrack(A_TRACK);

      expect(__player.calls('add')).toHaveLength(1);
    });
  });

  describe('native queue ops in flight at a queue switch (#1731)', () => {
    const loadQueueB = () => loadNativeQueue([B_TRACK], 0, { autoplay: false });

    it('an append started against the old queue does not add its track to the new one', async () => {
      const append = appendNativeTrack(A_TRACK);
      await loadQueueB();
      await append;

      expect(nativeAddedIds()).toEqual([trackKey(B_TRACK)]);
    });

    it('an insert-next started against the old queue does not add its track to the new one', async () => {
      const insert = insertNativeTrackNext(A_TRACK, 1);
      await loadQueueB();
      await insert;

      expect(nativeAddedIds()).toEqual([trackKey(B_TRACK)]);
    });

    it('an upcoming reorder started against the old queue leaves the new one intact', async () => {
      const reorder = reorderUpcomingNative([A_TRACK]);
      await loadQueueB();
      await reorder;

      expect(__player.calls('removeUpcomingTracks')).toHaveLength(0);
      expect(nativeAddedIds()).toEqual([trackKey(B_TRACK)]);
    });

    it('an append started after the switch reaches the new queue', async () => {
      await loadQueueB();
      await appendNativeTrack(A_TRACK);

      expect(nativeAddedIds()).toEqual([trackKey(B_TRACK), trackKey(A_TRACK)]);
    });
  });
});

describe('the presign window', () => {
  function makeLibrary(count: number): PlaybackTrack[] {
    return Array.from({ length: count }, (_, i) =>
      libraryTrack({
        source: { kind: 'library', trackId: asTrackId(`t${i}`) },
        title: `Track t${i}`,
      }),
    );
  }

  function presignedTrackIds(): Set<string> {
    const ids = new Set<string>();
    for (const req of __http.requests as { path: string; body?: string }[]) {
      if (req.path !== '/v1/audio-urls' || typeof req.body !== 'string') continue;
      const parsed = JSON.parse(req.body) as { track_ids?: string[] };
      for (const id of parsed.track_ids ?? []) ids.add(id);
    }
    return ids;
  }

  function addedTrackKeys(): string[][] {
    return (__player.calls('add') as unknown[][]).map(([arg]) =>
      (Array.isArray(arg) ? arg : [arg]).map((t) => (t as { id: string }).id),
    );
  }

  function loadedQueue(count: number, startIndex: number): Promise<void> {
    useQueueStore.getState().loadQueue(makeLibrary(count), startIndex, null);
    return loadNativeQueue(orderedQueueTracks(useQueueStore.getState()), startIndex, {
      autoplay: false,
    });
  }

  beforeEach(() => {
    useQueueStore.getState().clearQueue();
    __http.reply('POST /v1/audio-urls', { json: { urls: [] } });
  });

  describe('refreshUpcomingPresign — presign window slides beyond the first 25 as the queue advances', () => {
    it('presigns only the first 25 tracks at queue start', async () => {
      const library = makeLibrary(60);
      useQueueStore.getState().loadQueue(library, 0, null);

      await loadNativeQueue(orderedQueueTracks(useQueueStore.getState()), 0, { autoplay: false });

      const presigned = presignedTrackIds();
      expect(presigned.has('t0')).toBe(true);
      expect(presigned.has('t24')).toBe(true);
      expect(presigned.has('t30')).toBe(false);
    });

    it('presigns a track beyond the first 25 once the queue advances near the window edge', async () => {
      const library = makeLibrary(60);
      useQueueStore.getState().loadQueue(library, 0, null);
      await loadNativeQueue(orderedQueueTracks(useQueueStore.getState()), 0, { autoplay: false });
      expect(presignedTrackIds().has('t30')).toBe(false);

      useQueueStore.getState().skipToIndex(20);
      await refreshUpcomingPresign(20);

      expect(presignedTrackIds().has('t30')).toBe(true);
    });

    it('does not re-presign while the active track is still deep inside the presigned window', async () => {
      const library = makeLibrary(60);
      useQueueStore.getState().loadQueue(library, 0, null);
      await loadNativeQueue(orderedQueueTracks(useQueueStore.getState()), 0, { autoplay: false });
      const requestsAfterLoad = __http.countFor('POST /v1/audio-urls');

      useQueueStore.getState().skipToIndex(5);
      await refreshUpcomingPresign(5);

      expect(__http.countFor('POST /v1/audio-urls')).toBe(requestsAfterLoad);
    });
  });

  const RESTORED_QUEUE_LENGTH = 2000;

  function appendedTrack(): PlaybackTrack {
    return libraryTrack({
      source: { kind: 'library', trackId: asTrackId('t-appended') },
      title: 'Appended',
    });
  }

  describe('the native queue window — a queue far larger than the presign window', () => {
    it('marshals one window at load, not the whole queue', async () => {
      await loadedQueue(RESTORED_QUEUE_LENGTH, 0);

      const [added] = addedTrackKeys();
      expect(added).toHaveLength(NATIVE_QUEUE_WINDOW + 1);
      expect(added?.at(-1)).toBe(`library:t${NATIVE_QUEUE_WINDOW}`);
    });

    it('keeps every position before the active track, so a native index is still a queue index', async () => {
      await loadedQueue(RESTORED_QUEUE_LENGTH, 300);

      const [added] = addedTrackKeys();
      expect(added?.[300]).toBe('library:t300');
      expect(added).toHaveLength(301 + NATIVE_QUEUE_WINDOW);
    });

    it('slides the window forward on a presign refresh instead of re-pushing the remaining queue', async () => {
      await loadedQueue(RESTORED_QUEUE_LENGTH, 0);
      const addsAtLoad = addedTrackKeys().length;

      useQueueStore.getState().skipToIndex(20);
      await refreshUpcomingPresign(20);

      const slid = addedTrackKeys()[addsAtLoad];
      expect(slid).toHaveLength(NATIVE_QUEUE_WINDOW);
      expect(slid?.[0]).toBe('library:t21');
      expect(slid?.at(-1)).toBe(`library:t${20 + NATIVE_QUEUE_WINDOW}`);
    });

    it('leaves an append beyond the window edge to the next slide, so it cannot play early', async () => {
      await loadedQueue(RESTORED_QUEUE_LENGTH, 0);
      const addsAtLoad = addedTrackKeys().length;

      useQueueStore.getState().enqueue(appendedTrack());
      await appendNativeTrack(appendedTrack());

      expect(addedTrackKeys()).toHaveLength(addsAtLoad);
    });

    it('appends to the native queue while the whole queue still fits inside the window', async () => {
      await loadedQueue(3, 0);

      useQueueStore.getState().enqueue(appendedTrack());
      await appendNativeTrack(appendedTrack());

      expect(addedTrackKeys().at(-1)).toEqual(['library:t-appended']);
    });
  });

  const NATIVE_QUEUE_TIMEOUT_MESSAGE = 'Playback command timed out after 15s';

  function failNextReorder(): void {
    __player.failNext('add', new Error(NATIVE_QUEUE_TIMEOUT_MESSAGE));
  }

  function settled(): Promise<void> {
    return new Promise((resolve) => setImmediate(resolve));
  }

  describe('a presign slide whose native reorder rejects', () => {
    it('leaves the window unmarked, so the next active-track change slides it again', async () => {
      await loadedQueue(60, 0);
      useQueueStore.getState().skipToIndex(20);
      failNextReorder();
      await expect(refreshUpcomingPresign(20)).rejects.toThrow(NATIVE_QUEUE_TIMEOUT_MESSAGE);

      const presignsAfterFailure = __http.countFor('POST /v1/audio-urls');
      await refreshUpcomingPresign(20);

      expect(__http.countFor('POST /v1/audio-urls')).toBe(presignsAfterFailure + 1);
      expect(addedTrackKeys().at(-1)?.[0]).toBe('library:t21');
    });

    it('marks the window only once a slide has installed the URLs', async () => {
      await loadedQueue(60, 0);
      useQueueStore.getState().skipToIndex(20);
      failNextReorder();
      await expect(refreshUpcomingPresign(20)).rejects.toThrow(NATIVE_QUEUE_TIMEOUT_MESSAGE);
      await refreshUpcomingPresign(20);

      const presignsAfterRetry = __http.countFor('POST /v1/audio-urls');
      await refreshUpcomingPresign(20);

      expect(__http.countFor('POST /v1/audio-urls')).toBe(presignsAfterRetry);
    });
  });

  describe('the playback service reacting to a failed presign slide', () => {
    function onActiveTrackChanged(): (data: { index: number; track: { id: string } }) => void {
      const registration = __player
        .calls('addEventListener')
        .find(([event]: [unknown]) => event === Event.PlaybackActiveTrackChanged);
      if (!registration) throw new Error('no PlaybackActiveTrackChanged listener was registered');
      return registration[1];
    }

    afterEach(() => {
      usePlaybackErrorStore.getState().clear();
    });

    it('reports the failure against the playing track instead of dropping it', async () => {
      await loadedQueue(60, 0);
      await playbackService();
      failNextReorder();

      onActiveTrackChanged()({ index: 20, track: { id: 'library:t20' } });
      await settled();

      expect(usePlaybackErrorStore.getState()).toMatchObject({
        key: 'library:t20',
        kind: 'queue_update_failed',
      });
    });
  });
});

describe('presign failure trace', () => {
  const player = TrackPlayer as unknown as { getQueue: jest.Mock; add: jest.Mock; load: jest.Mock };

  function track(trackId: string): PlaybackTrack {
    return libraryTrack({ source: { kind: 'library', trackId: asTrackId(trackId) } });
  }

  function resolved(trackId: string): ResolvedAudioUrl {
    return { trackId, url: `https://cdn.example/${trackId}.mp3`, version: 'v1' };
  }

  let warn: jest.SpyInstance;

  beforeEach(() => {
    forgetAllSwaps();
    useQueueStore.getState().clearQueue();
    fetchUrls.mockReset();
    fetchUrls.mockImplementation(async (ids) => ids.map(resolved));
    player.getQueue.mockResolvedValue([]);
    warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
  });

  afterEach(() => {
    jest.restoreAllMocks();
  });

  describe('failure traces — a presigned URL never reaches the log', () => {
    const SIGNED_URL =
      'https://audio.altune.example/tracks/t1.m4a?X-Amz-Signature=deadbeefcafe&token=s3cr3t-token';

    function everythingLogged(): string {
      return JSON.stringify(warn.mock.calls);
    }

    it('redacts a signed URL a presign rejection carries, keeping its classification', async () => {
      fetchUrls.mockRejectedValue(new ApiError(403, `GET ${SIGNED_URL} denied`));
      const tracks = [track('t0'), track('t1')];
      useQueueStore.getState().loadQueue(tracks, 0, null);

      await loadNativeQueue(tracks, 0, { autoplay: false });

      expect(warn).toHaveBeenCalledWith('[playback] presign failed', {
        trackIds: ['t0', 't1'],
        error: { kind: 'auth', message: 'GET [redacted url] denied' },
      });
      expect(everythingLogged()).not.toContain('s3cr3t-token');
    });
  });

  describe('loadNativeQueue — presign failure trace', () => {
    it('logs the track ids it could not presign and still loads the queue', async () => {
      const boom = new Error('presign 503');
      fetchUrls.mockRejectedValue(boom);
      const tracks = [track('t0'), track('t1')];
      useQueueStore.getState().loadQueue(tracks, 0, null);

      await loadNativeQueue(tracks, 0, { autoplay: false });

      expect(warn).toHaveBeenCalledWith('[playback] presign failed', {
        trackIds: ['t0', 't1'],
        error: { kind: 'unknown', message: 'presign 503' },
      });
      expect(player.add).toHaveBeenCalled();
    });
  });
});

describe('insertNativeTrackNext', () => {
  beforeEach(() => {
    fetchUrls.mockImplementation(async () => []);
  });

  const player = TrackPlayer as unknown as Record<string, jest.Mock>;

  function track(id: string): PlaybackTrack {
    return libraryTrack({ source: { kind: 'library', trackId: asTrackId(id) } });
  }

  const A = track('a');
  const B = track('b');
  const C = track('c');

  afterEach(() => {
    restorePlayerDefault('add');
    restorePlayerDefault('getQueue');
    useQueueStore.getState().clearQueue();
  });

  describe('insertNativeTrackNext with a duplicate of the next track (#2703)', () => {
    it('adds the duplicate when native does not yet hold it', async () => {
      useQueueStore.getState().loadQueue([A, B, C], 0, null);
      let native: { id: string }[] = [A, B, C].map((t) => ({ id: trackKey(t) }));
      player.getQueue!.mockImplementation(async () => native);
      player.add!.mockImplementation(async (added: { id: string }, at: number) => {
        native = [...native.slice(0, at), added, ...native.slice(at)];
      });

      useQueueStore.getState().playNext(B);
      await insertNativeTrackNext(B, 1);

      expect(native.map((n) => n.id)).toEqual([A, B, B, C].map(trackKey));
    });
  });
});

describe('reorderUpcomingNative against native auto-advance and reorder bursts', () => {
  beforeEach(() => {
    fetchUrls.mockImplementation(async () => []);
  });

  type NativeItem = { id: string };
  const player = TrackPlayer as unknown as Record<string, jest.Mock>;
  const mockedFetchUrls = fetchAudioUrls as jest.MockedFunction<typeof fetchAudioUrls>;

  let nativeQueue: NativeItem[];
  let nativeIndex: number;
  let tailRebuilds: number;

  function modelNativePlayer(items: readonly PlaybackTrack[], active: number): void {
    nativeQueue = items.map((t) => ({ id: trackKey(t) }));
    nativeIndex = active;
    tailRebuilds = 0;
    player.add!.mockImplementation(async (added: NativeItem | NativeItem[]) => {
      nativeQueue = [...nativeQueue, ...(Array.isArray(added) ? added : [added])];
    });
    player.removeUpcomingTracks!.mockImplementation(async () => {
      tailRebuilds += 1;
      nativeQueue = nativeQueue.slice(0, nativeIndex + 1);
    });
    player.getActiveTrack!.mockImplementation(async () => nativeQueue[nativeIndex]);
    player.getActiveTrackIndex!.mockImplementation(async () => nativeIndex);
  }

  function nativeAutoAdvance(): void {
    nativeIndex += 1;
    const active = nativeQueue[nativeIndex]!;
    useQueueStore.getState().syncCurrentIndex(nativeIndex, active.id);
  }

  function deferredUrls(): () => void {
    let release!: () => void;
    const gate = new Promise<ResolvedAudioUrl[]>((resolve) => {
      release = () => resolve([]);
    });
    mockedFetchUrls.mockImplementationOnce(() => gate);
    return release;
  }

  async function flush(): Promise<void> {
    for (let i = 0; i < 10; i++) await Promise.resolve();
  }

  function track(id: string): PlaybackTrack {
    return libraryTrack({ source: { kind: 'library', trackId: asTrackId(id) } });
  }

  const A = track('a');
  const B = track('b');
  const C = track('c');
  const D = track('d');

  afterEach(() => {
    for (const name of [
      'add',
      'removeUpcomingTracks',
      'getActiveTrack',
      'getActiveTrackIndex',
    ] as const) {
      restorePlayerDefault(name);
    }
    mockedFetchUrls.mockReset();
    mockedFetchUrls.mockImplementation(async () => []);
    useQueueStore.getState().clearQueue();
  });

  describe('reorderUpcomingNative racing a native auto-advance (#816)', () => {
    it('does not duplicate the track native advanced to while URLs were resolving', async () => {
      useQueueStore.getState().loadQueue([A, B, C, D], 0, null);
      useQueueStore.getState().reorderQueue(2, 1);
      modelNativePlayer([A, B, C, D], 0);
      const release = deferredUrls();

      const reorder = reorderUpcomingNative([C, B, D]);
      await flush();
      nativeAutoAdvance();
      release();
      await reorder;

      const keys = nativeQueue.map((item) => item.id);
      expect(new Set(keys).size).toBe(keys.length);
      expect(keys).toEqual([A, B, D].map(trackKey));
      expect(nativeQueue[nativeIndex]!.id).toBe(trackKey(B));
      const s = useQueueStore.getState();
      expect(keys.slice(nativeIndex + 1)).toEqual(
        orderedQueueTracks(s)
          .slice(s.currentIndex + 1)
          .map(trackKey),
      );
    });

    it('re-adds the whole reordered list when native did not advance', async () => {
      modelNativePlayer([A, B, C, D], 0);
      const release = deferredUrls();

      const reorder = reorderUpcomingNative([C, B, D]);
      await flush();
      release();
      await reorder;

      expect(nativeQueue.map((item) => item.id)).toEqual([A, C, B, D].map(trackKey));
    });

    it('falls back to the whole list when the active track cannot be read', async () => {
      modelNativePlayer([A, B, C, D], 0);
      player.getActiveTrack!.mockRejectedValue(new Error('bridge down'));

      await reorderUpcomingNative([C, B, D]);

      expect(nativeQueue.map((item) => item.id)).toEqual([A, C, B, D].map(trackKey));
    });

    it('keeps a queued-twice copy of the playing track when native did not advance', async () => {
      modelNativePlayer([A, B, A], 0);

      await reorderUpcomingNative([A, B]);

      expect(nativeQueue.map((item) => item.id)).toEqual([A, A, B].map(trackKey));
    });
  });

  describe('reorderUpcomingNative coalescing a burst of reorder taps (#1735)', () => {
    function moveInStore(fromIndex: number, toIndex: number): readonly PlaybackTrack[] {
      return useQueueStore.getState().reorderQueue(fromIndex, toIndex);
    }

    function storedOrder(): string[] {
      return orderedQueueTracks(useQueueStore.getState()).map(trackKey);
    }

    beforeEach(() => {
      useQueueStore.getState().loadQueue([A, B, C, D], 0, null);
      modelNativePlayer([A, B, C, D], 0);
    });

    it('rebuilds the native tail once for three moves fired in one tick', async () => {
      await Promise.all([
        reorderUpcomingNative(moveInStore(1, 3)),
        reorderUpcomingNative(moveInStore(1, 2)),
        reorderUpcomingNative(moveInStore(3, 1)),
      ]);

      expect(tailRebuilds).toBe(1);
      expect(nativeQueue.map((item) => item.id)).toEqual([A, B, D, C].map(trackKey));
      expect(nativeQueue.map((item) => item.id)).toEqual(storedOrder());
    });

    it('collapses moves made while a rebuild is in flight onto one trailing rebuild', async () => {
      const release = deferredUrls();

      const firstMove = reorderUpcomingNative(moveInStore(1, 3));
      await flush();
      const duringRebuild = [
        reorderUpcomingNative(moveInStore(3, 1)),
        reorderUpcomingNative(moveInStore(2, 3)),
        reorderUpcomingNative(moveInStore(1, 2)),
      ];
      release();
      await Promise.all([firstMove, ...duringRebuild]);

      expect(tailRebuilds).toBe(2);
      expect(nativeQueue.map((item) => item.id)).toEqual([A, D, B, C].map(trackKey));
      expect(nativeQueue.map((item) => item.id)).toEqual(storedOrder());
    });

    it('resolves a superseded caller only once native holds the newer order', async () => {
      const release = deferredUrls();

      const supersededMove = reorderUpcomingNative(moveInStore(1, 3));
      await flush();
      void reorderUpcomingNative(moveInStore(1, 2));
      release();
      await supersededMove;

      expect(nativeQueue.map((item) => item.id)).toEqual([A, D, C, B].map(trackKey));
      expect(nativeQueue.map((item) => item.id)).toEqual(storedOrder());
    });
  });
});

describe('reorderUpcomingNative against the live store queue', () => {
  beforeEach(() => {
    fetchUrls.mockImplementation(async () => []);
  });

  type NativeItem = { id: string };
  const player = TrackPlayer as unknown as Record<string, jest.Mock>;
  const mockedFetchUrls = fetchAudioUrls as jest.MockedFunction<typeof fetchAudioUrls>;

  let nativeQueue: NativeItem[];
  let nativeIndex: number;

  function modelNativePlayer(items: readonly PlaybackTrack[], active: number): void {
    nativeQueue = items.map((t) => ({ id: trackKey(t) }));
    nativeIndex = active;
    player.add!.mockImplementation(async (added: NativeItem | NativeItem[], at?: number) => {
      const list = Array.isArray(added) ? added : [added];
      const position = at ?? nativeQueue.length;
      nativeQueue = [...nativeQueue.slice(0, position), ...list, ...nativeQueue.slice(position)];
    });
    player.getQueue!.mockImplementation(async () => nativeQueue);
    player.remove!.mockImplementation(async (index: number) => {
      nativeQueue = nativeQueue.filter((_, i) => i !== index);
    });
    player.removeUpcomingTracks!.mockImplementation(async () => {
      nativeQueue = nativeQueue.slice(0, nativeIndex + 1);
    });
    player.getActiveTrack!.mockImplementation(async () => nativeQueue[nativeIndex]);
    player.getActiveTrackIndex!.mockImplementation(async () => nativeIndex);
  }

  function deferredUrls(): () => void {
    let release!: () => void;
    const gate = new Promise<ResolvedAudioUrl[]>((resolve) => {
      release = () => resolve([]);
    });
    mockedFetchUrls.mockImplementationOnce(() => gate);
    return release;
  }

  async function flush(): Promise<void> {
    for (let i = 0; i < 10; i++) await Promise.resolve();
  }

  function track(id: string): PlaybackTrack {
    return libraryTrack({ source: { kind: 'library', trackId: asTrackId(id) } });
  }

  const A = track('a');
  const B = track('b');
  const C = track('c');
  const D = track('d');
  const E = track('e');

  function nativeKeys(): string[] {
    return nativeQueue.map((item) => item.id);
  }

  afterEach(() => {
    for (const name of [
      'add',
      'remove',
      'getQueue',
      'removeUpcomingTracks',
      'getActiveTrack',
      'getActiveTrackIndex',
    ] as const) {
      restorePlayerDefault(name);
    }
    mockedFetchUrls.mockReset();
    mockedFetchUrls.mockImplementation(async () => []);
    useQueueStore.getState().clearQueue();
  });

  describe('reorderUpcomingNative applies the live store queue at the lock (#2703)', () => {
    it('keeps native equal to the store when the user skips back during the slide', async () => {
      useQueueStore.getState().loadQueue([A, B, C, D], 1, null);
      modelNativePlayer([A, B, C, D], 1);
      const release = deferredUrls();

      const slide = reorderUpcomingNative([C, D]);
      await flush();
      nativeIndex = 0;
      useQueueStore.getState().syncCurrentIndex(0, trackKey(A));
      release();
      await slide;

      expect(nativeKeys()).toEqual(orderedQueueTracks(useQueueStore.getState()).map(trackKey));
    });

    it('keeps a track played next during the rebuild', async () => {
      useQueueStore.getState().loadQueue([A, B, C, D], 0, null);
      modelNativePlayer([A, B, C, D], 0);
      const release = deferredUrls();

      const slide = reorderUpcomingNative([B, C, D]);
      await flush();
      useQueueStore.getState().playNext(E);
      const insert = insertNativeTrackNext(E, 1);
      release();
      await Promise.all([slide, insert]);

      expect(nativeKeys()).toEqual([A, E, B, C, D].map(trackKey));
    });

    it('does not re-add a track removed during the rebuild', async () => {
      useQueueStore.getState().loadQueue([A, B, C, D], 0, null);
      modelNativePlayer([A, B, C, D], 0);
      const release = deferredUrls();

      const slide = reorderUpcomingNative([B, C, D]);
      await flush();
      useQueueStore.getState().removeFromQueue(2);
      await TrackPlayer.remove(2);
      release();
      await slide;

      expect(nativeKeys()).toEqual([A, B, D].map(trackKey));
    });
  });

  describe('reorderUpcomingNative keeps the native queue whole when the rebuild cannot finish', () => {
    it('restores the previous upcoming tracks when the add rejects, and surfaces the failure', async () => {
      useQueueStore.getState().loadQueue([A, B, C, D], 0, null);
      modelNativePlayer([A, B, C, D], 0);
      const addError = new Error('bridge dropped');
      const realAdd = player.add!.getMockImplementation()!;
      player.add!.mockImplementationOnce(async () => {
        throw addError;
      });

      await expect(reorderUpcomingNative([C, B, D])).rejects.toBe(addError);

      expect(nativeKeys()).toEqual([A, B, C, D].map(trackKey));
      player.add!.mockImplementation(realAdd);
    });

    it('does not leave the queue emptied when the token goes stale after the remove', async () => {
      useQueueStore.getState().loadQueue([A, B, C, D], 0, null);
      modelNativePlayer([A, B, C, D], 0);
      const removeUpcoming = player.removeUpcomingTracks!.getMockImplementation()!;
      player.removeUpcomingTracks!.mockImplementationOnce(async () => {
        claimLoad();
        await removeUpcoming();
      });

      await reorderUpcomingNative([C, B, D]);

      expect(nativeKeys()).toEqual([A, B, C, D].map(trackKey));
    });
  });

  describe('refreshUpcomingPresign only marks the window when the rebuild applied', () => {
    function library(count: number): PlaybackTrack[] {
      return Array.from({ length: count }, (_, i) => track(`p${i}`));
    }

    it('re-presigns on the next refresh after a rebuild that went stale', async () => {
      const tracks = library(60);
      useQueueStore.getState().loadQueue(tracks, 20, null);
      modelNativePlayer(tracks.slice(0, 21), 20);
      const release = deferredUrls();

      const stale = refreshUpcomingPresign(20);
      await flush();
      claimLoad();
      release();
      await stale;
      mockedFetchUrls.mockClear();
      await refreshUpcomingPresign(20);

      expect(mockedFetchUrls).toHaveBeenCalled();
    });

    it('skips the next refresh once a rebuild applied', async () => {
      const tracks = library(60);
      useQueueStore.getState().loadQueue(tracks, 20, null);
      modelNativePlayer(tracks.slice(0, 21), 20);

      await refreshUpcomingPresign(20);
      mockedFetchUrls.mockClear();
      await refreshUpcomingPresign(20);

      expect(mockedFetchUrls).not.toHaveBeenCalled();
    });
  });
});

describe('loadNativeQueue presign window', () => {
  function track(trackId: string): PlaybackTrack {
    return libraryTrack({ source: { kind: 'library', trackId: asTrackId(trackId) } });
  }

  it('presigns only from startIndex while still adding the tracks before it', async () => {
    const tracks = ['a', 'b', 'c'].map(track);
    fetchUrls.mockReset();
    fetchUrls.mockImplementation(async (ids) =>
      ids.map((trackId) => ({ trackId, url: `https://cdn.example/${trackId}.mp3`, version: 'v1' })),
    );

    await loadNativeQueue(tracks, 1, { autoplay: false });

    expect(fetchUrls).toHaveBeenCalledTimes(1);
    expect(fetchUrls).toHaveBeenCalledWith(['b', 'c']);
    const added = (TrackPlayer.add as jest.Mock).mock.calls.at(-1)?.[0] as { id: string }[];
    expect(added.map((t) => t.id)).toEqual(tracks.map(trackKey));
  });

  it('keeps auth headers on a library track before startIndex when only previews follow', async () => {
    const tracks = [track('a'), previewTrack()];
    fetchUrls.mockReset();
    fetchUrls.mockResolvedValue([]);

    await loadNativeQueue(tracks, 1, { autoplay: false });

    const added = (TrackPlayer.add as jest.Mock).mock.calls.at(-1)?.[0] as {
      headers?: Record<string, string>;
    }[];
    expect(Object.keys(added[0]?.headers ?? {}).length).toBeGreaterThan(0);
  });
});

describe('a tail rebuild that outlived the lock deadline', () => {
  const player = TrackPlayer as unknown as Record<string, jest.Mock>;

  function track(id: string): PlaybackTrack {
    return libraryTrack({ source: { kind: 'library', trackId: asTrackId(id) } });
  }

  const A = track('a');
  const B = track('b');
  const C = track('c');
  const D = track('d');
  const E = track('e');

  let nativeQueue: { id: string }[];

  function modelNativePlayer(items: readonly PlaybackTrack[], active: number): void {
    nativeQueue = items.map((t) => ({ id: trackKey(t) }));
    player.add!.mockImplementation(async (added: { id: string } | { id: string }[]) => {
      nativeQueue = [...nativeQueue, ...(Array.isArray(added) ? added : [added])];
    });
    player.getQueue!.mockImplementation(async () => nativeQueue);
    player.removeUpcomingTracks!.mockImplementation(async () => {
      nativeQueue = nativeQueue.slice(0, active + 1);
    });
    player.getActiveTrack!.mockImplementation(async () => nativeQueue[active]);
    player.getActiveTrackIndex!.mockImplementation(async () => active);
  }

  beforeEach(() => {
    fetchUrls.mockImplementation(async () => []);
    jest.useFakeTimers();
  });

  afterEach(() => {
    jest.useRealTimers();
    for (const name of [
      'add',
      'getQueue',
      'removeUpcomingTracks',
      'getActiveTrack',
      'getActiveTrackIndex',
    ] as const) {
      restorePlayerDefault(name);
    }
    useQueueStore.getState().clearQueue();
  });

  describe('reorderUpcomingNative — a stalled getQueue that resumes after the next op started', () => {
    it('runs no remove or add itself and leaves the native tail equal to the store window', async () => {
      useQueueStore.getState().loadQueue([A, B, C, D], 1, null);
      modelNativePlayer([A, B, C, D], 1);
      let releaseStall!: () => void;
      const stalled = new Promise<{ id: string }[]>((resolve) => {
        releaseStall = () => resolve(nativeQueue);
      });
      player.getQueue!.mockImplementationOnce(() => stalled);

      const rebuild = reorderUpcomingNative([C, D]);
      const rebuildOutcome = rebuild.catch((err: unknown) => err);
      await jest.advanceTimersByTimeAsync(1);
      const next = withNativeQueue(async () => undefined);
      useQueueStore.getState().loadQueue([A, B, E], 1, null);

      await jest.advanceTimersByTimeAsync(NATIVE_QUEUE_OP_TIMEOUT_MS);
      await next;
      releaseStall();
      await jest.advanceTimersByTimeAsync(NATIVE_QUEUE_OP_TIMEOUT_MS);
      await rebuildOutcome;

      expect(nativeQueue.map((n) => n.id)).toEqual([A, B, E].map(trackKey));
      expect(__player.calls('removeUpcomingTracks')).toHaveLength(1);
    });
  });

  describe('insertNativeTrackNext — a stalled getQueue that resumes after the next op started', () => {
    it('makes no add call', async () => {
      let releaseStall!: () => void;
      const stalled = new Promise<{ id: string }[]>((resolve) => {
        releaseStall = () => resolve([]);
      });
      player.getQueue!.mockImplementationOnce(() => stalled);

      const insert = insertNativeTrackNext(A, 1);
      const insertOutcome = insert.catch((err: unknown) => err);
      await jest.advanceTimersByTimeAsync(1);
      const next = withNativeQueue(async () => undefined);

      await jest.advanceTimersByTimeAsync(NATIVE_QUEUE_OP_TIMEOUT_MS);
      await next;
      releaseStall();
      await jest.advanceTimersByTimeAsync(NATIVE_QUEUE_OP_TIMEOUT_MS);
      await insertOutcome;

      expect(__player.calls('add')).toHaveLength(0);
    });
  });
});
