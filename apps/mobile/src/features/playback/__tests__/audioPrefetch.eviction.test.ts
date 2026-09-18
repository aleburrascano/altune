// Regression for issue #820: prefetch eviction must use fresh queue state and must not race an
// in-flight download of a track that was invalidated meanwhile.

import * as FileSystem from 'expo-file-system';
import TrackPlayer from 'react-native-track-player';

import { fetchAudioUrls, type ResolvedAudioUrl } from '@shared/api-client/audio';
import { asTrackId } from '@shared/api-client/ids';
import { useQueueStore } from '@shared/playback/queueStore';
import { trackKey } from '@shared/playback/trackKey';
import type { PlaybackTrack } from '@shared/playback/types';

import { evictCached, prefetchNext } from '../audioPrefetch';
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

const { __fs, File } = FileSystem as unknown as {
  __fs: { seedFile(uri: string, contents: string): void; allFiles(): Record<string, string> };
  File: { downloadFileAsync: (url: string, dest: { uri: string }) => Promise<{ uri: string }> };
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
    evictCached('t1');
    gate.resolve();
    await run;

    expect(cachedNames()).toEqual([]);
    expect(player.add).not.toHaveBeenCalled();
    expect(wasSwappedToLocal('t1')).toBe(false);
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
    evictCached('t1');
    gate.resolve();
    await run;

    expect(cachedNames()).toEqual([]);
  });

  it('still deletes a track cached files right away when nothing is downloading it', () => {
    __fs.seedFile(`${CACHE_DIR_URI}/t1.v1.mp3`, 'a');

    evictCached('t1');

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
    evictCached('t1');
    gate.resolve();
    await run;

    await prefetchNext(0);

    expect(cachedNames()).toEqual(['t1.v1.mp3']);
    expect(wasSwappedToLocal('t1')).toBe(true);
  });
});
