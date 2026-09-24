// Regression for issue #821: prefetch downloads are bounded — a stalled download times out, a
// superseded one is cancelled, and an oversized one is abandoned — instead of running unchecked.

import * as FileSystem from 'expo-file-system';
import TrackPlayer from 'react-native-track-player';

import { fetchAudioUrls, type ResolvedAudioUrl } from '@shared/api-client/audio';
import { asTrackId } from '@shared/api-client/ids';
import { useQueueStore } from '@shared/playback/queueStore';
import { trackKey } from '@shared/playback/trackKey';
import type { PlaybackTrack } from '@shared/playback/types';

import { MAX_PREFETCH_FILE_BYTES } from '../audioCache';
import { PREFETCH_STALL_TIMEOUT_MS, prefetchNext } from '../audioPrefetch';
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
