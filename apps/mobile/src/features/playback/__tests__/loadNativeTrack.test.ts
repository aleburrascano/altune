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
} from '../loadNativeTrack';
import { claimLoad } from '../loadToken';
import { forgetAllSwaps } from '../nativeTrackSwap';
import { usePlaybackErrorStore } from '../playbackErrorStore';
import { NATIVE_QUEUE_WINDOW } from '../presignWindow';
import { playbackService, resetPlaybackForSignOut } from '../service';

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

// fetchAudioUrls is the real presign by default; the reorder and insert-next blocks below
// stub it per test to hold or skip the URL resolve.
jest.mock('@shared/api-client/audio', () => {
  const actual = jest.requireActual('@shared/api-client/audio');
  return { ...actual, fetchAudioUrls: jest.fn() };
});

const fetchUrls = fetchAudioUrls as jest.MockedFunction<typeof fetchAudioUrls>;
const realFetchAudioUrls = jest.requireActual<{ fetchAudioUrls: typeof fetchAudioUrls }>(
  '@shared/api-client/audio',
).fetchAudioUrls;

// Every block in this file shares one TrackPlayer double, and several blocks stub these methods
// with their own implementations. Re-arm each method's default implementation before every test
// so no block's stubs leak into another, whatever order the blocks run in.
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

// Regression for #812: a multi-track TrackPlayer.add that throws partway can leave
// a partial native queue out of step with queueStore, so later index-based skips and
// removes hit the wrong native item. loadNativeQueue must reset the native queue
// before the add error propagates.
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

// Regression for #828: POST /v1/audio-urls is the only per-call check that the caller may
// still stream a track. A failed presign used to fall back to the pinned (downloaded) file
// whatever the failure, so a 401/403 (access revoked, session rejected) still played the
// previously downloaded bytes. The distinction the load now draws:
// - authorization denied (401/403): never serve the pinned file; the track streams, and the
//   stream endpoint re-checks authorization itself.
// - network/offline (transport failure, timeout) or a server fault: the pinned file still
//   plays, so downloads keep working while genuinely offline.
describe('presign failure and pinned audio', () => {
  const PINNED_URI = 'file:///document/offline-audio/t1.mp3';
  const TRACK = libraryTrack({ source: { kind: 'library', trackId: asTrackId('t1') } });

  // Only a ready entry records the version it downloaded, so the read narrows on status first.
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

// An append, insert-next or upcoming reorder resolves signed URLs before it takes the
// native queue lock. Anything that replaces the queue it was computed against while it is
// in flight must supersede it — a sign-out (#827) or a switch to another queue (#1731) —
// or it lands the old queue's tracks on the native player.
describe('native queue ops superseded by a sign-out or queue switch', () => {
  const A_TRACK = libraryTrack({ source: { kind: 'library', trackId: asTrackId('trk-of-a') } });
  const B_TRACK = libraryTrack({ source: { kind: 'library', trackId: asTrackId('trk-of-b') } });

  // Which tracks the native player was handed, in call order (a multi-track add counts as
  // its tracks). A call count alone cannot say which queue an add came from.
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

  // A full requeue — "Play Now" on another playlist, retry(), play(track) — claims a load
  // token but bumps no session epoch, which was all these ops watched before #1731, so they
  // sailed past the switch and mutated the queue that replaced the one they were built for.
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

// Regression for issue #30 (secondary "same songs repeat" symptom): the native
// queue can hold the whole library, but only MAX_PRESIGN (25) upcoming tracks get
// a fresh signed URL at load time. Left alone, a long shuffle session eventually
// reaches unsigned tracks. refreshUpcomingPresign slides that window forward as
// the queue advances so tracks beyond the initial 25 get presigned before playing.
describe('the presign window', () => {
  function makeLibrary(count: number): PlaybackTrack[] {
    return Array.from({ length: count }, (_, i) =>
      libraryTrack({
        source: { kind: 'library', trackId: asTrackId(`t${i}`) },
        title: `Track t${i}`,
      }),
    );
  }

  // Every track id that has been sent to POST /v1/audio-urls so far (i.e. presigned).
  function presignedTrackIds(): Set<string> {
    const ids = new Set<string>();
    for (const req of __http.requests as { path: string; body?: string }[]) {
      if (req.path !== '/v1/audio-urls' || typeof req.body !== 'string') continue;
      const parsed = JSON.parse(req.body) as { track_ids?: string[] };
      for (const id of parsed.track_ids ?? []) ids.add(id);
    }
    return ids;
  }

  // The track ids each TrackPlayer.add call marshalled across the bridge, in call order.
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
      // A track well past the initial window is NOT presigned yet — this is the cap
      // that caused the repeats in the car.
      expect(presigned.has('t30')).toBe(false);
    });

    it('presigns a track beyond the first 25 once the queue advances near the window edge', async () => {
      const library = makeLibrary(60);
      useQueueStore.getState().loadQueue(library, 0, null);
      await loadNativeQueue(orderedQueueTracks(useQueueStore.getState()), 0, { autoplay: false });
      expect(presignedTrackIds().has('t30')).toBe(false);

      // The player has advanced to position 20 — within the refresh margin of the
      // initial window edge (position 24).
      useQueueStore.getState().skipToIndex(20);
      await refreshUpcomingPresign(20);

      // The window has slid forward: track 30, previously unsigned, is now presigned.
      expect(presignedTrackIds().has('t30')).toBe(true);
    });

    it('does not re-presign while the active track is still deep inside the presigned window', async () => {
      const library = makeLibrary(60);
      useQueueStore.getState().loadQueue(library, 0, null);
      await loadNativeQueue(orderedQueueTracks(useQueueStore.getState()), 0, { autoplay: false });
      const requestsAfterLoad = __http.countFor('POST /v1/audio-urls');

      // Position 5 is far from the window edge (24), so no fresh presign is needed.
      useQueueStore.getState().skipToIndex(5);
      await refreshUpcomingPresign(5);

      expect(__http.countFor('POST /v1/audio-urls')).toBe(requestsAfterLoad);
    });
  });

  // Regression for #1732: MAX_PRESIGN bounded only how many tracks got a signed URL, never
  // how many track objects crossed the bridge. A saved-queue restore can hold
  // REHYDRATE_LIMIT (2000) tracks, and every one of them was marshalled into a single
  // TrackPlayer.add at load and again on every presign refresh.
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

  // Regression for #1725: the window was marked presigned *before* the native reorder that
  // installs the signed URLs. A rejected reorder (queue-lock timeout, bridge error) left
  // `presignedThrough` covering a block whose URLs never arrived, so no later slide fired
  // and the failure was neither classified nor reported — the service `void`-ed the call.
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
  // Regression for issue #822: a failed prefetch or presign falls back to live streaming, but it
  // must leave a diagnostic trace (which track, which stage) instead of being swallowed silently.
  // Issue #1741 closed the two paths inside nativeTrackSwap that still swallowed theirs: a native
  // remove that fails mid-swap, and a presign that fails while repairing the active track.
  // Issue #1720 made the trace carry a redacted, classified failure instead of the raw rejection.

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

  // Issue #1720: a failed native download names the URL it could not fetch, so logging the
  // rejection whole put a live presigned URL — signature and token query params intact — into
  // Metro/adb output, crash reports and device bug reports.
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
  // This block ran against a stubbed presign that resolves no URLs.
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

// Regression (#816): reorderUpcomingNative resolves signed URLs before it takes the
// native queue lock. Native can auto-advance (PlaybackActiveTrackChanged) while that
// resolve is in flight; removeUpcomingTracks then trims relative to the NEW active
// track, and re-adding the stale `upcoming` list duplicated the track that just
// became active. Drives the real reorder against a small model of the native queue.
describe('reorderUpcomingNative against native auto-advance and reorder bursts', () => {
  // This block ran against a stubbed presign that resolves no URLs.
  beforeEach(() => {
    fetchUrls.mockImplementation(async () => []);
  });

  type NativeItem = { id: string };
  const player = TrackPlayer as unknown as Record<string, jest.Mock>;
  const mockedFetchUrls = fetchAudioUrls as jest.MockedFunction<typeof fetchAudioUrls>;

  let nativeQueue: NativeItem[];
  let nativeIndex: number;
  // How many times the tail was handed to native: one remove + add per rebuild.
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

  // Native finishing the active track on its own: the index moves and the service
  // syncs the store cursor from the PlaybackActiveTrackChanged payload.
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
      useQueueStore.getState().reorderQueue(2, 1); // store order: A*, C, B, D
      modelNativePlayer([A, B, C, D], 0);
      const release = deferredUrls();

      const reorder = reorderUpcomingNative([C, B, D]);
      await flush();
      nativeAutoAdvance(); // native: A -> B before the reorder reaches the lock
      release();
      await reorder;

      const keys = nativeQueue.map((item) => item.id);
      expect(new Set(keys).size).toBe(keys.length);
      expect(keys).toEqual([A, B, D].map(trackKey));
      expect(nativeQueue[nativeIndex]!.id).toBe(trackKey(B));
      // Native's upcoming tracks match the store's view after its cursor synced to B.
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

  // Regression (#1735): every "Move Up" tap and shuffle toggle handed its own upcoming list
  // to reorderUpcomingNative, so a burst cost one presign round trip and one full tail
  // rebuild per tap — each pushing an order the next tap had already replaced.
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
  // This block ran against a stubbed presign that resolves no URLs.
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
});
