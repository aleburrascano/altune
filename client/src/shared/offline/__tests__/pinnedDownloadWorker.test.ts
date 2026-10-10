import * as FileSystem from 'expo-file-system';

import { ApiError, NetworkError } from '@shared/failure/errors';
import {
  createMemoryFileStore,
  type MemoryFileStore,
} from '@shared/files/__tests__/memoryFileStore';

import { createMemoryOfflinePort } from '@shared/offline/__tests__/memoryOfflinePort';
import { DOWNLOAD_RETRY_BASE_MS, runDownloadQueue } from '@shared/offline/pinnedDownloadWorker';
import {
  MIN_FREE_BYTES,
  PIN_DOWNLOAD_TIMEOUT_MS,
  setPinnedFileStore,
} from '@shared/offline/pinnedFiles';
import type { OfflinePort } from '@shared/offline/ports/offlinePort';
import type { PinnedEntry } from '@shared/offline/pinnedIndex';
import { recordEvent } from '@shared/telemetry/recordEvent';
import { asTrackId, type TrackId } from '@shared/apiClient/ids';

jest.mock('@shared/telemetry/recordEvent', () => ({
  recordEvent: jest.fn(() => Promise.resolve()),
}));

const recordEventMock = recordEvent as jest.MockedFunction<typeof recordEvent>;
const seededUrls: Record<string, { url: string; version: string }> = {};
const memoryPort = createMemoryOfflinePort({ urls: seededUrls });
const fetchAudioUrlsMock = jest.fn(memoryPort.fetchAudioUrls);
const port: OfflinePort = { fetchAudioUrls: fetchAudioUrlsMock };

const { __fs } = FileSystem as unknown as {
  __fs: { reset(): void; readFile(uri: string): string | undefined };
};

const INDEX_URI = 'file:///document/offline/pinned.json';

type State = { entries: Record<string, PinnedEntry>; queue: TrackId[]; isWorking: boolean };
type Update = Partial<State> | ((s: State) => Partial<State>);

beforeEach(() => {
  __fs.reset();
  for (const trackId of Object.keys(seededUrls)) delete seededUrls[trackId];
  fetchAudioUrlsMock.mockReset().mockImplementation(memoryPort.fetchAudioUrls);
});

describe('runDownloadQueue', () => {
  it('never re-creates an entry that is removed between the worker reading and writing it', async () => {
    let state: State = {
      entries: { t1: { trackId: asTrackId('t1'), status: 'queued' } },
      queue: [asTrackId('t1')],
      isWorking: false,
    };
    const get = (): State => state;
    let updaters = 0;
    const set = (update: Update): void => {
      if (typeof update !== 'function') {
        state = { ...state, ...update };
        return;
      }
      updaters += 1;
      if (updaters === 2) state = { ...state, entries: {} };
      state = { ...state, ...update(state) };
    };

    await runDownloadQueue(set, get, port);

    expect(state.entries).toEqual({});
    expect(state.queue).toEqual([]);
    expect(state.isWorking).toBe(false);
    expect(__fs.readFile(INDEX_URI)).toBeUndefined();
  });
});

function queuedTrack(trackId: TrackId) {
  let state: State = {
    entries: { [trackId]: { trackId, status: 'queued' } },
    queue: [trackId],
    isWorking: false,
  };
  return {
    get: (): State => state,
    set: (update: Update): void => {
      state = { ...state, ...(typeof update === 'function' ? update(state) : update) };
    },
    get entry(): PinnedEntry | undefined {
      return state.entries[trackId];
    },
    unpin: (): void => {
      state = { ...state, entries: {}, queue: [] };
    },
  };
}

async function settleMicrotasks(rounds = 20): Promise<void> {
  for (let i = 0; i < rounds; i += 1) {
    await Promise.resolve();
  }
}

function signedUrl(trackId: string): string {
  return `https://cdn.example.com/audio/${trackId}.mp3?sig=secret`;
}

describe('a transient pinned-download failure is retried before the track is failed', () => {
  let store: MemoryFileStore;
  let warn: jest.SpyInstance;

  beforeEach(() => {
    store = createMemoryFileStore();
    setPinnedFileStore(store);
    warn = jest.spyOn(console, 'warn').mockImplementation(() => {});
    jest.useFakeTimers();
  });

  afterEach(() => {
    jest.useRealTimers();
    setPinnedFileStore();
    warn.mockRestore();
  });

  it('reattempts a dropped connection twice on a doubling backoff before marking the entry failed', async () => {
    const random = jest.spyOn(Math, 'random').mockReturnValue(0);
    fetchAudioUrlsMock.mockRejectedValue(new NetworkError('transport', 'the API is unreachable'));
    const track = queuedTrack(asTrackId('t1'));

    const drained = runDownloadQueue(track.set, track.get, port);
    await settleMicrotasks();
    expect(fetchAudioUrlsMock).toHaveBeenCalledTimes(1);

    for (const [attempt, wait] of [
      [2, DOWNLOAD_RETRY_BASE_MS / 2],
      [3, DOWNLOAD_RETRY_BASE_MS],
    ] as const) {
      await jest.advanceTimersByTimeAsync(wait - 1);
      expect(fetchAudioUrlsMock).toHaveBeenCalledTimes(attempt - 1);
      expect(track.entry?.status).toBe('downloading');
      await jest.advanceTimersByTimeAsync(1);
      expect(fetchAudioUrlsMock).toHaveBeenCalledTimes(attempt);
    }
    await drained;

    expect(fetchAudioUrlsMock).toHaveBeenCalledTimes(3);
    expect(track.entry?.status).toBe('failed');
    expect(warn).toHaveBeenCalledWith(
      expect.stringContaining(`retrying in ${DOWNLOAD_RETRY_BASE_MS / 2}ms`),
      expect.anything(),
    );
    random.mockRestore();
  });

  it('ends ready when a transient 5xx clears on a retry, so no re-tap of pin is needed', async () => {
    seededUrls['t1'] = { url: signedUrl('t1'), version: 'v3' };
    fetchAudioUrlsMock.mockRejectedValueOnce(new ApiError(503, 'audio urls unavailable'));
    const track = queuedTrack(asTrackId('t1'));

    const drained = runDownloadQueue(track.set, track.get, port);
    await jest.advanceTimersByTimeAsync(DOWNLOAD_RETRY_BASE_MS);
    await drained;

    expect(track.entry).toEqual({
      trackId: 't1',
      status: 'ready',
      uri: 'memory://document/offline-audio/t1.mp3',
      version: 'v3',
    });
    expect(store.files.get('memory://document/offline-audio/t1.mp3')).toContain(signedUrl('t1'));
  });

  it('fails a track on its first attempt when pinned storage is full, without waiting to retry', async () => {
    store.freeBytes = MIN_FREE_BYTES - 1;
    const track = queuedTrack(asTrackId('t1'));

    const drained = runDownloadQueue(track.set, track.get, port);
    await settleMicrotasks();

    expect(track.entry?.status).toBe('failed');
    expect(fetchAudioUrlsMock).not.toHaveBeenCalled();
    await drained;
  });

  it('stops retrying a track unpinned during its backoff wait', async () => {
    fetchAudioUrlsMock.mockRejectedValue(new NetworkError('transport', 'the API is unreachable'));
    const track = queuedTrack(asTrackId('t1'));

    const drained = runDownloadQueue(track.set, track.get, port);
    await settleMicrotasks();
    track.unpin();
    await jest.advanceTimersByTimeAsync(DOWNLOAD_RETRY_BASE_MS * 4);
    await drained;

    expect(fetchAudioUrlsMock).toHaveBeenCalledTimes(1);
    expect(track.get().entries).toEqual({});
  });
});

describe('a failed byte transfer is retried like any other transient failure', () => {
  let store: MemoryFileStore;
  let warn: jest.SpyInstance;

  beforeEach(() => {
    __fs.reset();
    seededUrls['t1'] = { url: 'https://cdn.example.com/t1.mp3?sig=x', version: 'v1' };
    store = createMemoryFileStore();
    setPinnedFileStore(store);
    warn = jest.spyOn(console, 'warn').mockImplementation(() => {});
    jest.useFakeTimers();
  });

  afterEach(() => {
    jest.useRealTimers();
    setPinnedFileStore();
    warn.mockRestore();
  });

  it('ends ready when a dropped connection clears on the second transfer', async () => {
    const realDownload = store.download;
    let calls = 0;
    store.download = (url, dest, signal) => {
      calls += 1;
      return calls === 1 ? Promise.reject(new Error('net')) : realDownload(url, dest, signal);
    };
    const track = queuedTrack(asTrackId('t1'));

    const drained = runDownloadQueue(track.set, track.get, port);
    await jest.advanceTimersByTimeAsync(10_000);
    await drained;

    expect(fetchAudioUrlsMock).toHaveBeenCalledTimes(2);
    expect(track.entry?.status).toBe('ready');
  });

  it('retries a timed-out transfer and stops at three attempts', async () => {
    store.download = () => new Promise(() => {});
    const track = queuedTrack(asTrackId('t1'));

    const drained = runDownloadQueue(track.set, track.get, port);
    await jest.advanceTimersByTimeAsync(PIN_DOWNLOAD_TIMEOUT_MS * 3 + 30_000);
    await drained;

    expect(fetchAudioUrlsMock).toHaveBeenCalledTimes(3);
    expect(track.entry?.status).toBe('failed');
  });

  it('does not retry when storage is full', async () => {
    store.freeBytes = MIN_FREE_BYTES - 1;
    const track = queuedTrack(asTrackId('t1'));

    const drained = runDownloadQueue(track.set, track.get, port);
    await jest.advanceTimersByTimeAsync(10_000);
    await drained;

    expect(fetchAudioUrlsMock).not.toHaveBeenCalled();
    expect(track.entry?.status).toBe('failed');
  });
});

describe('download_failed telemetry', () => {
  beforeEach(() => {
    setPinnedFileStore(createMemoryFileStore());
    jest.spyOn(console, 'warn').mockImplementation(() => {});
    jest.useFakeTimers();
    recordEventMock.mockReset().mockResolvedValue(undefined);
  });

  afterEach(() => {
    jest.useRealTimers();
    setPinnedFileStore();
    jest.restoreAllMocks();
  });

  function eventsFor(trackId: string) {
    return recordEventMock.mock.calls
      .map(([e]) => e)
      .filter((e) => e.type === 'download_failed' && e.payload?.['track_id'] === trackId);
  }

  it('sends one download_failed per failed attempt, retrying until the last, with no signed URL', async () => {
    const store = createMemoryFileStore();
    setPinnedFileStore(store);
    store.download = () => Promise.reject(new Error('net'));
    seededUrls['tel-1'] = { url: signedUrl('tel-1'), version: 'v1' };
    const track = queuedTrack(asTrackId('tel-1'));

    const drained = runDownloadQueue(track.set, track.get, port);
    await jest.advanceTimersByTimeAsync(DOWNLOAD_RETRY_BASE_MS * 10);
    await drained;

    expect(eventsFor('tel-1')).toEqual([
      { type: 'download_failed', payload: { track_id: 'tel-1', will_retry: true } },
      { type: 'download_failed', payload: { track_id: 'tel-1', will_retry: true } },
      { type: 'download_failed', payload: { track_id: 'tel-1', will_retry: false } },
    ]);
    expect(JSON.stringify(recordEventMock.mock.calls)).not.toContain('sig=secret');
  });

  it('swallows a failed telemetry send so the download still fails cleanly', async () => {
    recordEventMock.mockRejectedValue(new Error('offline'));
    fetchAudioUrlsMock.mockRejectedValue(new ApiError(404, 'gone'));
    const track = queuedTrack(asTrackId('tel-2'));

    await runDownloadQueue(track.set, track.get, port);

    expect(track.entry?.status).toBe('failed');
    expect(eventsFor('tel-2')).toHaveLength(1);
  });
});
