// Regression for issue #2525: a download the app died in the middle of must never be taken for a
// finished cache file, so prefetch writes under a temporary name and renames only on success.

import * as FileSystem from 'expo-file-system';
import TrackPlayer from 'react-native-track-player';

import { fetchAudioUrls, type ResolvedAudioUrl } from '@shared/api-client/audio';
import { asTrackId } from '@shared/api-client/ids';
import { useQueueStore } from '@shared/playback/queueStore';
import { trackKey } from '@shared/playback/trackKey';
import type { PlaybackTrack } from '@shared/playback/types';

import { evict, findCached } from '../audioCache';
import { prefetchNext } from '../audioPrefetch';
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
  for (const mock of [player.getQueue, player.remove, player.add]) mock.mockReset();
  warn.mockRestore();
});

describe('findCached — a download left unfinished by a killed app', () => {
  it('is not returned as a cache hit', () => {
    __fs.seedFile(LEFTOVER_URI, 'trunc');

    expect(findCached(asTrackId('t1'), 'v1')).toBeNull();
  });

  it('does not shadow the finished file of the same track and version', () => {
    __fs.seedFile(LEFTOVER_URI, 'trunc');
    __fs.seedFile(FINAL_URI, 'whole');

    expect(findCached(asTrackId('t1'), 'v1')?.uri).toBe(FINAL_URI);
  });
});

describe('evict — a download left unfinished by a killed app', () => {
  it('is purged once its track leaves the retention window', () => {
    __fs.seedFile(LEFTOVER_URI, 'trunc');

    evict([track('t9')], 0);

    expect(cachedNames()).toEqual([]);
  });
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
