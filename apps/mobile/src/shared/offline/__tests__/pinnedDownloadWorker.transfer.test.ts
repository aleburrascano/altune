import * as FileSystem from 'expo-file-system';

import { fetchAudioUrls } from '@shared/api-client/audio';
import { asTrackId, type TrackId } from '@shared/api-client/ids';
import {
  createMemoryFileStore,
  type MemoryFileStore,
} from '@shared/files/__tests__/memoryFileStore';

import { runDownloadQueue } from '../pinnedDownloadWorker';
import { MIN_FREE_BYTES, PIN_DOWNLOAD_TIMEOUT_MS, setPinnedFileStore } from '../pinnedFiles';
import type { PinnedEntry } from '../pinnedIndex';

jest.mock('@shared/api-client/audio', () => ({ fetchAudioUrls: jest.fn() }));

const fetchAudioUrlsMock = fetchAudioUrls as jest.MockedFunction<typeof fetchAudioUrls>;
const { __fs } = FileSystem as unknown as { __fs: { reset(): void } };

type State = { entries: Record<string, PinnedEntry>; queue: TrackId[]; isWorking: boolean };
type Update = Partial<State> | ((s: State) => Partial<State>);

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
  };
}

describe('a failed byte transfer is retried like any other transient failure', () => {
  let store: MemoryFileStore;
  let warn: jest.SpyInstance;

  beforeEach(() => {
    __fs.reset();
    fetchAudioUrlsMock.mockReset();
    fetchAudioUrlsMock.mockResolvedValue([
      { trackId: 't1', url: 'https://cdn.example.com/t1.mp3?sig=x', version: 'v1' },
    ]);
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

    const drained = runDownloadQueue(track.set, track.get);
    await jest.advanceTimersByTimeAsync(10_000);
    await drained;

    expect(fetchAudioUrlsMock).toHaveBeenCalledTimes(2);
    expect(track.entry?.status).toBe('ready');
  });

  it('retries a timed-out transfer and stops at three attempts', async () => {
    store.download = () => new Promise(() => {});
    const track = queuedTrack(asTrackId('t1'));

    const drained = runDownloadQueue(track.set, track.get);
    await jest.advanceTimersByTimeAsync(PIN_DOWNLOAD_TIMEOUT_MS * 3 + 30_000);
    await drained;

    expect(fetchAudioUrlsMock).toHaveBeenCalledTimes(3);
    expect(track.entry?.status).toBe('failed');
  });

  it('does not retry when storage is full', async () => {
    store.freeBytes = MIN_FREE_BYTES - 1;
    const track = queuedTrack(asTrackId('t1'));

    const drained = runDownloadQueue(track.set, track.get);
    await jest.advanceTimersByTimeAsync(10_000);
    await drained;

    expect(fetchAudioUrlsMock).not.toHaveBeenCalled();
    expect(track.entry?.status).toBe('failed');
  });
});
