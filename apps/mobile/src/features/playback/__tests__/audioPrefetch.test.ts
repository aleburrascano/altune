import * as FileSystem from 'expo-file-system';
import { posix } from 'path';
import TrackPlayer from 'react-native-track-player';

import { fetchAudioUrls, type ResolvedAudioUrl } from '@shared/api-client/audio';
import { asTrackId, parseTrackId, type TrackId } from '@shared/api-client/ids';
import { useQueueStore } from '@shared/playback/queueStore';
import { trackKey } from '@shared/playback/trackKey';
import type { PlaybackTrack } from '@shared/playback/types';

import * as audioCache from '../audioCache';
import { MAX_PREFETCH_FILE_BYTES, cacheDir, findCached } from '../audioCache';
import { PREFETCH_STALL_TIMEOUT_MS, evictCached, prefetchNext } from '../audioPrefetch';
import { forgetAllSwaps, wasSwappedToLocal } from '../nativeTrackSwap';

import { libraryTrack } from './fixtures';

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: {
    auth: {
      getSession: jest
        .fn()
        .mockResolvedValue({ data: { session: { access_token: 'tok' } }, error: null }),
    },
  },
}));

jest.mock('@shared/api-client/audio', () => ({
  ...jest.requireActual('@shared/api-client/audio'),
  fetchAudioUrls: jest.fn(),
}));

// The kill-switch and cache-path tests drive the real fetchAudioUrls against the fetch double;
// the rest stub it per test.
const realFetchAudioUrls: typeof fetchAudioUrls = jest.requireActual(
  '@shared/api-client/audio',
).fetchAudioUrls;

function useRealFetchAudioUrls(): void {
  (fetchAudioUrls as jest.MockedFunction<typeof fetchAudioUrls>).mockImplementation(
    realFetchAudioUrls,
  );
}

// Every block in this file shares one TrackPlayer double, and several blocks stub these methods
// with their own implementations. Re-arm each method's default implementation before every test
// so no block's stubs leak into another, whatever order the blocks run in.
const nativePlayer = TrackPlayer as unknown as Record<string, jest.Mock>;
const STUBBED_PLAYER_METHODS = ['add', 'getQueue', 'load', 'remove'] as const;
const defaultPlayerImpls = new Map(
  STUBBED_PLAYER_METHODS.map((name) => [name, nativePlayer[name]!.getMockImplementation()]),
);

function restorePlayerDefault(name: (typeof STUBBED_PLAYER_METHODS)[number]): void {
  nativePlayer[name]!.mockReset().mockImplementation(defaultPlayerImpls.get(name));
}

beforeEach(() => {
  for (const name of STUBBED_PLAYER_METHODS) restorePlayerDefault(name);
});

describe('bounded downloads', () => {
  // Regression for issue #821: prefetch downloads are bounded — a stalled download times out, a
  // superseded one is cancelled, and an oversized one is abandoned — instead of running unchecked.

  interface DownloadOptions {
    signal?: AbortSignal;
    onProgress?: (data: { bytesWritten: number; totalBytes: number }) => void;
  }

  type Download = (
    url: string,
    dest: { uri: string },
    options?: DownloadOptions,
  ) => Promise<{ uri: string }>;

  const { __fs, File } = FileSystem as unknown as {
    __fs: { seedFile(uri: string, contents: string): void; allFiles(): Record<string, string> };
    File: { downloadFileAsync: Download };
  };

  const player = TrackPlayer as unknown as { getQueue: jest.Mock; add: jest.Mock };
  const fetchUrls = fetchAudioUrls as jest.MockedFunction<typeof fetchAudioUrls>;

  const CACHE_DIR_URI = 'file:///cache/audio-prefetch';

  function track(trackId: string): PlaybackTrack {
    return libraryTrack({ source: { kind: 'library', trackId: asTrackId(trackId) } });
  }

  function resolved(trackId: string): ResolvedAudioUrl {
    return { trackId, url: `https://cdn.example/${trackId}.mp3`, version: 'v1' };
  }

  function cachedNames(): string[] {
    return Object.keys(__fs.allFiles())
      .map((uri) => uri.slice(CACHE_DIR_URI.length + 1))
      .sort();
  }

  async function flushMicrotasks(): Promise<void> {
    for (let i = 0; i < 20; i++) await Promise.resolve();
  }

  // A native download that has stalled: it writes a partial file, reports it started, and then
  // never settles — not even when its abort signal fires.
  function stallForever(started: string[], signals: AbortSignal[] = []) {
    return (_url: string, dest: { uri: string }, options?: DownloadOptions) => {
      __fs.seedFile(dest.uri, 'partial');
      started.push(dest.uri);
      if (options?.signal) signals.push(options.signal);
      return new Promise<{ uri: string }>(() => {});
    };
  }

  function trackSettled(run: Promise<void>): () => boolean {
    let settled = false;
    void run.then(() => {
      settled = true;
    });
    return () => settled;
  }

  beforeEach(() => {
    forgetAllSwaps();
    useQueueStore.getState().clearQueue();
    fetchUrls.mockReset();
    fetchUrls.mockImplementation(async ([id]) => [resolved(id!)]);
  });

  afterEach(async () => {
    // An empty queue has no next track, so this supersedes any download a test left hanging.
    useQueueStore.getState().clearQueue();
    await prefetchNext(0);
    await flushMicrotasks();
    jest.useRealTimers();
    jest.restoreAllMocks();
  });

  describe('prefetchNext — stalled download', () => {
    it('times out, clears the in-flight marker and removes the partial file', async () => {
      jest.useFakeTimers();
      const active = track('t0');
      const next = track('t1');
      useQueueStore.getState().loadQueue([active, next], 0, null);
      player.getQueue.mockResolvedValue([{ id: trackKey(active) }, { id: trackKey(next) }]);

      const started: string[] = [];
      const realDownload = File.downloadFileAsync;
      const download = jest
        .spyOn(File, 'downloadFileAsync')
        .mockImplementationOnce(stallForever(started));

      const settled = trackSettled(prefetchNext(0));
      await flushMicrotasks();
      expect(started).toHaveLength(1);

      jest.advanceTimersByTime(PREFETCH_STALL_TIMEOUT_MS);
      await flushMicrotasks();

      expect(settled()).toBe(true);
      expect(cachedNames()).toEqual([]);

      // The track is no longer pinned in flight: a later prefetch downloads and swaps it.
      download.mockImplementation(realDownload);
      await prefetchNext(0);
      expect(download).toHaveBeenCalledTimes(2);
      expect(cachedNames()).toEqual(['t1.v1.mp3']);
      expect(wasSwappedToLocal(asTrackId('t1'))).toBe(true);
    });

    it('does not time out a slow download that keeps making progress', async () => {
      jest.useFakeTimers();
      useQueueStore.getState().loadQueue([track('t0'), track('t1')], 0, null);
      player.getQueue.mockResolvedValue([]);

      let progress: DownloadOptions['onProgress'];
      let finish!: () => void;
      let signal: AbortSignal | undefined;
      jest.spyOn(File, 'downloadFileAsync').mockImplementationOnce((_url, dest, options) => {
        progress = options?.onProgress;
        signal = options?.signal;
        return new Promise((resolve) => {
          finish = () => {
            __fs.seedFile(dest.uri, 'done');
            resolve({ uri: dest.uri });
          };
        });
      });

      const settled = trackSettled(prefetchNext(0));
      await flushMicrotasks();
      for (let i = 1; i <= 5; i++) {
        jest.advanceTimersByTime(PREFETCH_STALL_TIMEOUT_MS - 1);
        progress?.({ bytesWritten: i * 1024, totalBytes: 10 * 1024 });
      }
      expect(signal?.aborted).toBe(false);

      finish();
      await flushMicrotasks();
      expect(settled()).toBe(true);
      expect(cachedNames()).toEqual(['t1.v1.mp3']);
    });
  });

  describe('prefetchNext — superseded download', () => {
    it('cancels the download of a track skipped past before it finished', async () => {
      const ids = ['t0', 't1', 't2', 't3'];
      useQueueStore.getState().loadQueue(ids.map(track), 0, null);
      player.getQueue.mockResolvedValue(ids.map((id) => ({ id: trackKey(track(id)) })));

      const started: string[] = [];
      const signals: AbortSignal[] = [];
      const realDownload = File.downloadFileAsync;
      jest
        .spyOn(File, 'downloadFileAsync')
        .mockImplementationOnce(stallForever(started, signals))
        .mockImplementation(realDownload);

      const settled = trackSettled(prefetchNext(0));
      await flushMicrotasks();
      expect(started).toEqual([`${CACHE_DIR_URI}/t1.v1.mp3.part`]);

      // The user skips ahead: t2 is next now, so the t1 download is stale.
      useQueueStore.getState().skipToIndex(1);
      await prefetchNext(1);
      await flushMicrotasks();

      expect(signals[0]?.aborted).toBe(true);
      expect(settled()).toBe(true);
      expect(cachedNames()).toEqual(['t2.v1.mp3']);
      expect(wasSwappedToLocal(asTrackId('t1'))).toBe(false);
      expect(wasSwappedToLocal(asTrackId('t2'))).toBe(true);
    });

    it('leaves the download running when the same track is still next', async () => {
      useQueueStore.getState().loadQueue([track('t0'), track('t1')], 0, null);
      player.getQueue.mockResolvedValue([]);

      const started: string[] = [];
      const signals: AbortSignal[] = [];
      const download = jest
        .spyOn(File, 'downloadFileAsync')
        .mockImplementation(stallForever(started, signals));

      void prefetchNext(0);
      await flushMicrotasks();
      await prefetchNext(0);

      expect(download).toHaveBeenCalledTimes(1);
      expect(signals[0]?.aborted).toBe(false);
    });
  });

  describe('prefetchNext — oversized download', () => {
    it('abandons a download that grows past the per-file byte cap', async () => {
      const active = track('t0');
      const next = track('t1');
      useQueueStore.getState().loadQueue([active, next], 0, null);
      player.getQueue.mockResolvedValue([{ id: trackKey(active) }, { id: trackKey(next) }]);

      const signals: AbortSignal[] = [];
      jest.spyOn(File, 'downloadFileAsync').mockImplementationOnce((_url, dest, options) => {
        __fs.seedFile(dest.uri, 'partial');
        if (options?.signal) signals.push(options.signal);
        options?.onProgress?.({ bytesWritten: MAX_PREFETCH_FILE_BYTES + 1, totalBytes: -1 });
        return new Promise(() => {});
      });

      await prefetchNext(0);

      expect(signals[0]?.aborted).toBe(true);
      expect(cachedNames()).toEqual([]);
      expect(player.add).not.toHaveBeenCalled();
      expect(wasSwappedToLocal(asTrackId('t1'))).toBe(false);
    });
  });
});

describe('eviction', () => {
  // Regression for issue #820: prefetch eviction must use fresh queue state and must not race an
  // in-flight download of a track that was invalidated meanwhile.

  const { __fs, File } = FileSystem as unknown as {
    __fs: { seedFile(uri: string, contents: string): void; allFiles(): Record<string, string> };
    File: { downloadFileAsync: (url: string, dest: { uri: string }) => Promise<{ uri: string }> };
  };

  const player = TrackPlayer as unknown as { getQueue: jest.Mock; add: jest.Mock };
  type NativeAdd = [{ id: string; url: string }, number?];
  const fetchUrls = fetchAudioUrls as jest.MockedFunction<typeof fetchAudioUrls>;

  const CACHE_DIR_URI = 'file:///cache/audio-prefetch';

  function track(trackId: string): PlaybackTrack {
    return libraryTrack({ source: { kind: 'library', trackId: asTrackId(trackId) } });
  }

  function resolved(trackId: string): ResolvedAudioUrl {
    return { trackId, url: `https://cdn.example/${trackId}.mp3`, version: 'v1' };
  }

  // What the player would play for a track: the URL of the last native slot written for it.
  // Cache eviction deletes files, never native slots, so this outlives the file it points at.
  function nativeUrlOf(track: PlaybackTrack): string | undefined {
    const writes = (player.add.mock.calls as NativeAdd[]).filter(
      ([native]) => native.id === trackKey(track),
    );
    return writes.at(-1)?.[0].url;
  }

  function cachedNames(): string[] {
    return Object.keys(__fs.allFiles())
      .map((uri) => uri.slice(CACHE_DIR_URI.length + 1))
      .sort();
  }

  function deferred(): { promise: Promise<void>; resolve: () => void } {
    let resolve!: () => void;
    const promise = new Promise<void>((r) => {
      resolve = r;
    });
    return { promise, resolve };
  }

  const ids = ['t0', 't1', 't2', 't3', 't4', 't5', 't6', 't7'];

  beforeEach(() => {
    forgetAllSwaps();
    useQueueStore.getState().clearQueue();
    fetchUrls.mockReset();
  });

  afterEach(() => {
    jest.restoreAllMocks();
  });

  describe('prefetchNext — cache-hit eviction uses fresh queue state', () => {
    it('does not evict the new window when the active track advances during the url fetch', async () => {
      useQueueStore.getState().loadQueue(ids.map(track), 0, null);
      for (const id of ids) __fs.seedFile(`${CACHE_DIR_URI}/${id}.v1.mp3`, id);
      player.getQueue.mockResolvedValue([]);

      fetchUrls.mockImplementationOnce(async ([id]) => {
        useQueueStore.getState().skipToIndex(3);
        return [resolved(id!)];
      });

      await prefetchNext(0);

      expect(cachedNames()).toEqual([
        't3.v1.mp3',
        't4.v1.mp3',
        't5.v1.mp3',
        't6.v1.mp3',
        't7.v1.mp3',
      ]);
    });
  });

  describe('prefetchNext — invalidation racing an in-flight download', () => {
    it('neither keeps nor swaps in a download whose track was invalidated mid-flight', async () => {
      const active = track('t0');
      const next = track('t1');
      useQueueStore.getState().loadQueue([active, next], 0, null);
      player.getQueue.mockResolvedValue([{ id: trackKey(active) }, { id: trackKey(next) }]);
      fetchUrls.mockResolvedValueOnce([resolved('t1')]);

      const gate = deferred();
      const started = deferred();
      const realDownload = File.downloadFileAsync;
      jest.spyOn(File, 'downloadFileAsync').mockImplementation(async (url, dest) => {
        started.resolve();
        await gate.promise;
        return realDownload(url, dest);
      });

      const run = prefetchNext(0);
      await started.promise;
      evictCached(asTrackId('t1'));
      gate.resolve();
      await run;

      expect(cachedNames()).toEqual([]);
      expect(player.add).not.toHaveBeenCalled();
      expect(wasSwappedToLocal(asTrackId('t1'))).toBe(false);
    });

    it('removes a partial file left by a download that fails after the track was invalidated', async () => {
      useQueueStore.getState().loadQueue([track('t0'), track('t1')], 0, null);
      fetchUrls.mockResolvedValueOnce([resolved('t1')]);

      const gate = deferred();
      const started = deferred();
      jest.spyOn(File, 'downloadFileAsync').mockImplementation(async (_url, dest) => {
        __fs.seedFile(dest.uri, 'partial');
        started.resolve();
        await gate.promise;
        throw new Error('connection reset');
      });

      const run = prefetchNext(0);
      await started.promise;
      evictCached(asTrackId('t1'));
      gate.resolve();
      await run;

      expect(cachedNames()).toEqual([]);
    });

    it('still deletes a track cached files right away when nothing is downloading it', () => {
      __fs.seedFile(`${CACHE_DIR_URI}/t1.v1.mp3`, 'a');

      evictCached(asTrackId('t1'));

      expect(cachedNames()).toEqual([]);
    });

    it('a later prefetch of the invalidated track downloads and swaps normally', async () => {
      const active = track('t0');
      const next = track('t1');
      useQueueStore.getState().loadQueue([active, next], 0, null);
      player.getQueue.mockResolvedValue([{ id: trackKey(active) }, { id: trackKey(next) }]);
      fetchUrls.mockResolvedValue([resolved('t1')]);

      const gate = deferred();
      const started = deferred();
      const realDownload = File.downloadFileAsync;
      jest.spyOn(File, 'downloadFileAsync').mockImplementationOnce(async (url, dest) => {
        started.resolve();
        await gate.promise;
        return realDownload(url, dest);
      });

      const run = prefetchNext(0);
      await started.promise;
      evictCached(asTrackId('t1'));
      gate.resolve();
      await run;

      await prefetchNext(0);

      expect(cachedNames()).toEqual(['t1.v1.mp3']);
      expect(wasSwappedToLocal(asTrackId('t1'))).toBe(true);
    });
  });

  describe('the swapped-to-local set against the routine eviction pass', () => {
    // #1734 asked for the opposite: reclaim the entry when the window pass drops the file. The
    // slot the swap wrote is still in the native queue pointing at that file, so the entry is
    // what routes the next error on the track to `repairActiveToStreaming` — the one recovery
    // that reloads a dangling local slot. `recoverAudio`, the branch without it, only asks the
    // server to re-derive the audio and leaves playback stopped.
    it('keeps the entry for a track whose file the window pass dropped under its native slot', async () => {
      const queue = ids.map(track);
      useQueueStore.getState().loadQueue(queue, 0, null);
      player.getQueue.mockResolvedValue(queue.map((t) => ({ id: trackKey(t) })));
      fetchUrls.mockImplementation(async ([id]) => [resolved(id!)]);
      await prefetchNext(0);
      const swappedSlotUrl = nativeUrlOf(queue[1]!);

      useQueueStore.getState().skipToIndex(6);
      await prefetchNext(6);

      expect(swappedSlotUrl).toBe(`${CACHE_DIR_URI}/t1.v1.mp3`);
      expect(cachedNames()).not.toContain('t1.v1.mp3');
      expect(nativeUrlOf(queue[1]!)).toBe(swappedSlotUrl);
      expect(wasSwappedToLocal(asTrackId('t1'))).toBe(true);
    });
  });
});

describe('cache file construction', () => {
  const fetchUrls = fetchAudioUrls as jest.MockedFunction<typeof fetchAudioUrls>;

  function track(trackId: string) {
    return libraryTrack({ source: { kind: 'library', trackId: asTrackId(trackId) } });
  }

  describe('prefetchNext cache file construction failure', () => {
    it('is traced as the download stage', async () => {
      forgetAllSwaps();
      useQueueStore.getState().clearQueue();
      fetchUrls.mockImplementation(async (ids) =>
        ids.map((id) => ({ trackId: id, url: `https://cdn.example/${id}.mp3`, version: 'v1' })),
      );
      const warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
      jest.spyOn(audioCache, 'findCached').mockReturnValue(null);
      jest.spyOn(audioCache, 'cacheDir').mockImplementation(() => {
        throw new Error('no cache dir');
      });
      useQueueStore.getState().loadQueue([track('t0'), track('t1')], 0, null);

      await prefetchNext(0);

      expect(warn).toHaveBeenCalledWith(
        '[playback] prefetch failed',
        expect.objectContaining({ stage: 'download', trackId: 't1' }),
      );
      jest.restoreAllMocks();
    });
  });
});

describe('failure trace', () => {
  // Regression for issue #822: a failed prefetch or presign falls back to live streaming, but it
  // must leave a diagnostic trace (which track, which stage) instead of being swallowed silently.
  // Issue #1741 closed the two paths inside nativeTrackSwap that still swallowed theirs: a native
  // remove that fails mid-swap, and a presign that fails while repairing the active track.
  // Issue #1720 made the trace carry a redacted, classified failure instead of the raw rejection.

  type DownloadWithProgress = (
    url: string,
    dest: { uri: string },
    options?: { onProgress?: (data: { bytesWritten: number; totalBytes: number }) => void },
  ) => Promise<{ uri: string }>;

  const { File } = FileSystem as unknown as { File: { downloadFileAsync: DownloadWithProgress } };
  const player = TrackPlayer as unknown as { getQueue: jest.Mock; add: jest.Mock; load: jest.Mock };
  const { __player } = jest.requireMock('react-native-track-player') as {
    __player: { failNext: (method: string, error: Error) => void };
  };
  const fetchUrls = fetchAudioUrls as jest.MockedFunction<typeof fetchAudioUrls>;

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

  describe('prefetchNext — failure trace', () => {
    it('logs the track and stage when fetchAudioUrls throws', async () => {
      const boom = new Error('network down');
      fetchUrls.mockRejectedValue(boom);
      useQueueStore.getState().loadQueue([track('t0'), track('t1')], 0, null);

      await expect(prefetchNext(0)).resolves.toBeUndefined();

      expect(warn).toHaveBeenCalledWith('[playback] prefetch failed', {
        stage: 'resolve',
        trackId: 't1',
        error: { kind: 'unknown', message: 'network down' },
      });
    });

    it('logs a download that fails on its own (e.g. disk full)', async () => {
      const diskFull = new Error('ENOSPC');
      jest.spyOn(File, 'downloadFileAsync').mockRejectedValueOnce(diskFull);
      useQueueStore.getState().loadQueue([track('t0'), track('t1')], 0, null);

      await prefetchNext(0);

      expect(warn).toHaveBeenCalledWith('[playback] prefetch failed', {
        stage: 'download',
        trackId: 't1',
        error: { kind: 'unknown', message: 'ENOSPC' },
      });
    });

    it('logs a download abandoned for growing past the byte cap, with the cause', async () => {
      jest.spyOn(File, 'downloadFileAsync').mockImplementationOnce(((_url, _dest, options) => {
        options?.onProgress?.({ bytesWritten: MAX_PREFETCH_FILE_BYTES + 1, totalBytes: -1 });
        return new Promise<{ uri: string }>(() => {});
      }) as DownloadWithProgress);
      useQueueStore.getState().loadQueue([track('t0'), track('t1')], 0, null);

      await prefetchNext(0);

      expect(warn).toHaveBeenCalledWith('[playback] prefetch failed', {
        stage: 'download',
        trackId: 't1',
        error: { kind: 'unknown', message: 'prefetch download aborted: oversized' },
      });
    });

    it('logs the swap stage when installing the downloaded file throws', async () => {
      const boom = new Error('cache dir not readable');
      jest.spyOn(audioCache, 'evict').mockImplementation(() => {
        throw boom;
      });
      useQueueStore.getState().loadQueue([track('t0'), track('t1')], 0, null);

      await prefetchNext(0);

      expect(warn).toHaveBeenCalledWith('[playback] prefetch failed', {
        stage: 'swap',
        trackId: 't1',
        error: { kind: 'unknown', message: 'cache dir not readable' },
      });
    });

    it('logs the swap stage when removing the upcoming native slot fails', async () => {
      const boom = new Error('native remove failed');
      const [active, next] = [track('t0'), track('t1')];
      useQueueStore.getState().loadQueue([active, next], 0, null);
      player.getQueue.mockResolvedValue([{ id: trackKey(active) }, { id: trackKey(next) }]);
      __player.failNext('remove', boom);

      await prefetchNext(0);

      expect(warn).toHaveBeenCalledWith('[playback] prefetch failed', {
        stage: 'swap',
        trackId: 't1',
        error: { kind: 'unknown', message: 'native remove failed' },
      });
    });

    it('stays quiet when a download is superseded by a skip', async () => {
      useQueueStore.getState().loadQueue(['t0', 't1', 't2', 't3'].map(track), 0, null);
      jest
        .spyOn(File, 'downloadFileAsync')
        .mockImplementationOnce(() => new Promise<{ uri: string }>(() => {}));

      const first = prefetchNext(0);
      for (let i = 0; i < 20; i++) await Promise.resolve();
      useQueueStore.getState().skipToIndex(1);
      await prefetchNext(1);
      await first;

      expect(warn).not.toHaveBeenCalledWith('[playback] prefetch failed', expect.anything());
    });

    it('stays quiet on a successful prefetch', async () => {
      useQueueStore.getState().loadQueue([track('t0'), track('t1')], 0, null);

      await prefetchNext(0);

      expect(warn).not.toHaveBeenCalled();
    });
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

    it('redacts the signed URL a failed download names', async () => {
      jest
        .spyOn(File, 'downloadFileAsync')
        .mockRejectedValueOnce(new Error(`Unable to open ${SIGNED_URL} (403)`));
      useQueueStore.getState().loadQueue([track('t0'), track('t1')], 0, null);

      await prefetchNext(0);

      expect(warn).toHaveBeenCalledWith('[playback] prefetch failed', {
        stage: 'download',
        trackId: 't1',
        error: { kind: 'unknown', message: 'Unable to open [redacted url] (403)' },
      });
      expect(everythingLogged()).not.toContain('deadbeefcafe');
    });

    it('redacts a rejection thrown as a bare string', async () => {
      jest
        .spyOn(File, 'downloadFileAsync')
        .mockRejectedValueOnce(`download of ${SIGNED_URL} failed`);
      useQueueStore.getState().loadQueue([track('t0'), track('t1')], 0, null);

      await prefetchNext(0);

      expect(warn).toHaveBeenCalledWith('[playback] prefetch failed', {
        stage: 'download',
        trackId: 't1',
        error: { kind: 'unknown', message: 'download of [redacted url] failed' },
      });
      expect(everythingLogged()).not.toContain('deadbeefcafe');
    });

    it('logs only the type of a rejection that is not an Error, never its fields', async () => {
      jest.spyOn(File, 'downloadFileAsync').mockRejectedValueOnce({ requestUrl: SIGNED_URL });
      useQueueStore.getState().loadQueue([track('t0'), track('t1')], 0, null);

      await prefetchNext(0);

      expect(warn).toHaveBeenCalledWith('[playback] prefetch failed', {
        stage: 'download',
        trackId: 't1',
        error: { kind: 'unknown', message: 'non-Error rejection: object' },
      });
      expect(everythingLogged()).not.toContain('deadbeefcafe');
    });
  });
});

describe('cache path containment', () => {
  // Regression for issue #826: a trackId (or audio version) carrying path syntax must never let a
  // prefetch download land outside the audio cache directory.

  const { __http } = require('../../../../jest/doubles/fetch.js');
  const { __fs } = FileSystem as unknown as { __fs: { allFiles(): Record<string, string> } };

  function track(trackId: string): PlaybackTrack {
    // A cast, not asTrackId: the hostile ids below must reach prefetch past the brand (#944).
    return libraryTrack({ source: { kind: 'library', trackId: trackId as TrackId } });
  }

  function normalizedPath(uri: string): string {
    return posix.normalize(uri.replace(/^file:\/\//, ''));
  }

  async function prefetchSecondTrack(trackId: string, version = 'v1'): Promise<string[]> {
    useQueueStore.getState().loadQueue([track('active'), track(trackId)], 0, null);
    __http.reply('POST /v1/audio-urls', {
      json: { urls: [{ track_id: trackId, url: 'https://cdn.example/a.mp3', version }] },
    });
    await prefetchNext(0);
    return Object.keys(__fs.allFiles());
  }

  beforeEach(() => {
    useRealFetchAudioUrls();
    useQueueStore.getState().clearQueue();
  });

  describe('prefetchNext — cache path containment', () => {
    it('downloads a well-formed track id into the cache directory', async () => {
      const files = await prefetchSecondTrack('0b7c9a4e-1f2d-4c3b-9a8e-7d6f5e4c3b2a');

      expect(files).toEqual([`${cacheDir().uri}/0b7c9a4e-1f2d-4c3b-9a8e-7d6f5e4c3b2a.v1.mp3`]);
    });

    it.each(['../../../document/evil', '..', 'a/../../b'])(
      'keeps every written file inside cacheDir() for trackId %p',
      async (trackId) => {
        const files = await prefetchSecondTrack(trackId);

        const root = `${normalizedPath(cacheDir().uri)}/`;
        for (const uri of files) expect(normalizedPath(uri).startsWith(root)).toBe(true);
        expect(files).toEqual([]);
      },
    );

    it('rejects a server-supplied version carrying path syntax', async () => {
      const files = await prefetchSecondTrack('trk-2', '../../../document/evil');

      expect(files).toEqual([]);
    });
  });

  describe('parseTrackId', () => {
    it('accepts a UUID and the opaque token ids the client already uses', () => {
      expect(parseTrackId('0b7c9a4e-1f2d-4c3b-9a8e-7d6f5e4c3b2a')).toEqual({
        ok: true,
        id: '0b7c9a4e-1f2d-4c3b-9a8e-7d6f5e4c3b2a',
      });
      expect(parseTrackId('trk_42').ok).toBe(true);
    });

    it.each(['', '../x', 'a/b', 'a?b', 'a#b', 'a%2Fb', 'a.b', 'x'.repeat(129)])(
      'rejects %p with a typed failure',
      (value) => {
        expect(parseTrackId(value)).toEqual({
          ok: false,
          error: { kind: 'invalid-track-id', value },
        });
      },
    );
  });
});

describe('remote kill switch', () => {
  // Regression for issue #824: the server can pull audio prefetching remotely (AUDIO_PREFETCH_ENABLED
  // on the API, surfaced as `prefetch_enabled` on every /v1/audio-urls response). With the switch
  // off, `prefetchNext` must be a no-op so playback falls back to straight streaming.

  const { __http } = require('../../../../jest/doubles/fetch.js');
  const { __fs } = FileSystem as unknown as { __fs: { allFiles(): Record<string, string> } };

  function track(trackId: string): PlaybackTrack {
    return libraryTrack({ source: { kind: 'library', trackId: asTrackId(trackId) } });
  }

  function replyAudioUrls(prefetchEnabled?: boolean): void {
    __http.reply('POST /v1/audio-urls', {
      json: {
        urls: [{ track_id: 'trk-2', url: 'https://cdn.example/a.mp3', version: 'v1' }],
        ...(prefetchEnabled === undefined ? {} : { prefetch_enabled: prefetchEnabled }),
      },
    });
  }

  // Plays the resolve a track load does, so the client learns the server's current switch state.
  async function serverSays(prefetchEnabled: boolean | undefined): Promise<void> {
    replyAudioUrls(prefetchEnabled);
    await fetchAudioUrls(['trk-1']);
    __http.reset();
  }

  beforeEach(async () => {
    useRealFetchAudioUrls();
    await serverSays(true);
    useQueueStore.getState().clearQueue();
    useQueueStore.getState().loadQueue([track('trk-1'), track('trk-2')], 0, null);
  });

  // The switch is module state in the api client, shared with every other block in this file:
  // turn it back on so a test that left it off cannot silence prefetching elsewhere.
  afterEach(async () => {
    __http.reset();
    await serverSays(true);
  });

  describe('prefetchNext — remote kill switch', () => {
    it('prefetches the next track while the switch is on (the default)', async () => {
      replyAudioUrls(true);

      await prefetchNext(0);

      expect(Object.keys(__fs.allFiles())).toHaveLength(1);
    });

    it('treats a server that does not send the flag as enabled', async () => {
      await serverSays(undefined);
      replyAudioUrls();

      await prefetchNext(0);

      expect(Object.keys(__fs.allFiles())).toHaveLength(1);
    });

    it('is a no-op once the server has turned prefetching off: no request, no download', async () => {
      await serverSays(false);
      replyAudioUrls(false);

      await prefetchNext(0);

      expect(__http.countFor('POST /v1/audio-urls')).toBe(0);
      expect(Object.keys(__fs.allFiles())).toEqual([]);
    });

    it('downloads nothing when its own resolve reports the switch turned off', async () => {
      replyAudioUrls(false);

      await prefetchNext(0);

      expect(Object.keys(__fs.allFiles())).toEqual([]);
    });

    it('resumes prefetching when the server turns the switch back on', async () => {
      await serverSays(false);
      await prefetchNext(0);
      await serverSays(true);
      replyAudioUrls(true);

      await prefetchNext(0);

      expect(Object.keys(__fs.allFiles())).toHaveLength(1);
    });
  });
});

describe('a download left unfinished by a killed app', () => {
  interface DownloadOptions {
    signal?: AbortSignal;
  }

  type Download = (
    url: string,
    dest: { uri: string },
    options?: DownloadOptions,
  ) => Promise<{ uri: string }>;

  const { __fs, File } = FileSystem as unknown as {
    __fs: {
      seedFile(uri: string, contents: string): void;
      readFile(uri: string): string | undefined;
      allFiles(): Record<string, string>;
      failNext(kind: 'move', error?: Error): void;
    };
    File: { downloadFileAsync: Download };
  };

  const player = TrackPlayer as unknown as {
    getQueue: jest.Mock;
    remove: jest.Mock;
    add: jest.Mock;
  };
  const fetchUrls = fetchAudioUrls as jest.MockedFunction<typeof fetchAudioUrls>;

  const CACHE_DIR_URI = 'file:///cache/audio-prefetch';
  const FINAL_URI = `${CACHE_DIR_URI}/t1.v1.mp3`;
  const LEFTOVER_URI = `${CACHE_DIR_URI}/t1.v1.mp3.part`;

  interface NativeEntry {
    id: string;
    url: string;
  }

  function track(trackId: string): PlaybackTrack {
    return libraryTrack({ source: { kind: 'library', trackId: asTrackId(trackId) } });
  }

  function resolved(trackId: string): ResolvedAudioUrl {
    return { trackId, url: `https://cdn.example/${trackId}.mp3`, version: 'v1' };
  }

  function cachedNames(): string[] {
    return Object.keys(__fs.allFiles())
      .map((uri) => uri.slice(CACHE_DIR_URI.length + 1))
      .sort();
  }

  function modelNativeQueue(tracks: readonly PlaybackTrack[]): NativeEntry[] {
    const queue: NativeEntry[] = tracks.map((t) => ({
      id: trackKey(t),
      url: `https://stream.example/${t.title}`,
    }));
    player.getQueue.mockImplementation(async () => [...queue]);
    player.remove.mockImplementation(async (index: number) => {
      queue.splice(index, 1);
    });
    player.add.mockImplementation(async (entry: NativeEntry, index: number) => {
      queue.splice(index, 0, entry);
    });
    return queue;
  }

  function loadNextTrackQueue(): NativeEntry[] {
    const tracks = [track('t0'), track('t1')];
    useQueueStore.getState().loadQueue(tracks, 0, null);
    return modelNativeQueue(tracks);
  }

  async function flushMicrotasks(): Promise<void> {
    for (let i = 0; i < 20; i++) await Promise.resolve();
  }

  let warn: jest.SpyInstance;

  beforeEach(() => {
    forgetAllSwaps();
    useQueueStore.getState().clearQueue();
    fetchUrls.mockReset();
    fetchUrls.mockImplementation(async ([id]) => [resolved(id!)]);
    warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
  });

  afterEach(async () => {
    useQueueStore.getState().clearQueue();
    await prefetchNext(0);
    await flushMicrotasks();
    jest.restoreAllMocks();
    for (const name of ['getQueue', 'remove', 'add'] as const) restorePlayerDefault(name);
    warn.mockRestore();
  });

  describe('prefetchNext — download under a temporary name', () => {
    it('never swaps a leftover partial in: it downloads afresh and swaps the finished file', async () => {
      const queue = loadNextTrackQueue();
      __fs.seedFile(LEFTOVER_URI, 'trunc');

      await prefetchNext(0);

      expect(queue.map((entry) => entry.url)).toContain(FINAL_URI);
      expect(queue.map((entry) => entry.url)).not.toContain(LEFTOVER_URI);
      expect(__fs.readFile(FINAL_URI)).toBe('downloaded:https://cdn.example/t1.mp3');
      expect(cachedNames()).toEqual(['t1.v1.mp3']);
    });

    it('holds no file under the finished name while the download is still running', async () => {
      loadNextTrackQueue();
      jest.spyOn(File, 'downloadFileAsync').mockImplementationOnce((_url, dest) => {
        __fs.seedFile(dest.uri, 'partial');
        return new Promise<{ uri: string }>(() => {});
      });

      void prefetchNext(0);
      await flushMicrotasks();

      expect(findCached(asTrackId('t1'), 'v1')).toBeNull();
      expect(cachedNames()).toEqual(['t1.v1.mp3.part']);
    });

    it('leaves the track streaming and no file behind when the rename fails', async () => {
      const queue = loadNextTrackQueue();
      __fs.failNext('move', new Error('EXDEV: rename failed'));

      await prefetchNext(0);

      expect(cachedNames()).toEqual([]);
      expect(queue.map((entry) => entry.url)).not.toContain(FINAL_URI);
      expect(wasSwappedToLocal(asTrackId('t1'))).toBe(false);
    });
  });
});
