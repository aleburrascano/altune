import * as FileSystem from 'expo-file-system';

import { fetchAudioUrls } from '@shared/api-client/audio';
import { ApiError, NetworkError } from '@shared/errors';
import {
  createMemoryFileStore,
  type MemoryFileStore,
} from '@shared/files/__tests__/memoryFileStore';

import { DOWNLOAD_RETRY_BASE_MS, runDownloadQueue } from '../pinnedDownloadWorker';
import { MIN_FREE_BYTES, setPinnedFileStore } from '../pinnedFiles';
import type { PinnedEntry } from '../pinnedIndex';
import { asTrackId, type TrackId } from '@shared/api-client/ids';

jest.mock('@shared/api-client/audio', () => ({ fetchAudioUrls: jest.fn() }));

const fetchAudioUrlsMock = fetchAudioUrls as jest.MockedFunction<typeof fetchAudioUrls>;

const { __fs } = FileSystem as unknown as {
  __fs: { reset(): void; readFile(uri: string): string | undefined };
};

const INDEX_URI = 'file:///document/offline/pinned.json';

type State = { entries: Record<string, PinnedEntry>; queue: TrackId[]; isWorking: boolean };
type Update = Partial<State> | ((s: State) => Partial<State>);

beforeEach(() => {
  __fs.reset();
  fetchAudioUrlsMock.mockReset();
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
    // Simulates an unpin landing after the worker checked the entry but before its
    // 'downloading' mark (the second functional update, after the dequeue) applies.
    const set = (update: Update): void => {
      if (typeof update !== 'function') {
        state = { ...state, ...update };
        return;
      }
      updaters += 1;
      if (updaters === 2) state = { ...state, entries: {} };
      state = { ...state, ...update(state) };
    };
    fetchAudioUrlsMock.mockResolvedValue([]);

    await runDownloadQueue(set, get);

    expect(state.entries).toEqual({});
    expect(state.queue).toEqual([]);
    expect(state.isWorking).toBe(false);
    expect(__fs.readFile(INDEX_URI)).toBeUndefined();
  });
});

// One queued track and the setter/getter pair the store would hand the worker.
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

    const drained = runDownloadQueue(track.set, track.get);
    await settleMicrotasks();
    expect(fetchAudioUrlsMock).toHaveBeenCalledTimes(1);

    // random 0 => each wait is exactly half its ceiling: 1x then 2x base / 2.
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
    fetchAudioUrlsMock
      .mockRejectedValueOnce(new ApiError(503, 'audio urls unavailable'))
      .mockResolvedValue([{ trackId: 't1', url: signedUrl('t1'), version: 'v3' }]);
    const track = queuedTrack(asTrackId('t1'));

    const drained = runDownloadQueue(track.set, track.get);
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

    const drained = runDownloadQueue(track.set, track.get);
    await settleMicrotasks();

    expect(track.entry?.status).toBe('failed');
    expect(fetchAudioUrlsMock).not.toHaveBeenCalled();
    await drained;
  });

  it('stops retrying a track unpinned during its backoff wait', async () => {
    fetchAudioUrlsMock.mockRejectedValue(new NetworkError('transport', 'the API is unreachable'));
    const track = queuedTrack(asTrackId('t1'));

    const drained = runDownloadQueue(track.set, track.get);
    await settleMicrotasks();
    track.unpin();
    await jest.advanceTimersByTimeAsync(DOWNLOAD_RETRY_BASE_MS * 4);
    await drained;

    expect(fetchAudioUrlsMock).toHaveBeenCalledTimes(1);
    expect(track.get().entries).toEqual({});
  });
});
