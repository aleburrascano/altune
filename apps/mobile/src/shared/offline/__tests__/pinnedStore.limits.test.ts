import { act } from '@testing-library/react-native';

import { fetchAudioUrls, type ResolvedAudioUrl } from '@shared/api-client/audio';
import { asTrackId } from '@shared/api-client/ids';
import type { StoredDirectory, StoredFile } from '@shared/files/fileStore';
import {
  createMemoryFileStore,
  type MemoryFileStore,
} from '@shared/files/__tests__/memoryFileStore';

import {
  MAX_PINNED_BYTES,
  MIN_FREE_BYTES,
  PIN_DOWNLOAD_TIMEOUT_MS,
  setPinnedFileStore,
} from '../pinnedFiles';
import { usePinnedStore } from '../pinnedStore';

jest.mock('@shared/api-client/audio', () => ({ fetchAudioUrls: jest.fn() }));

const fetchAudioUrlsMock = fetchAudioUrls as jest.MockedFunction<typeof fetchAudioUrls>;

function resolved(trackId: string): ResolvedAudioUrl {
  return { trackId, url: `https://cdn.example.com/audio/${trackId}.mp3?sig=secret`, version: 'v1' };
}

async function flush(rounds = 40): Promise<void> {
  for (let i = 0; i < rounds; i += 1) {
    await Promise.resolve();
  }
}

// Reports every pinned file as `size` bytes, so a cap in the gigabytes can be reached without
// holding gigabytes of test data.
function withFileSize(store: MemoryFileStore, size: number): MemoryFileStore {
  const sized = (file: StoredFile): StoredFile => ({
    uri: file.uri,
    get exists() {
      return file.exists;
    },
    get size() {
      return file.exists ? size : null;
    },
    textSync: () => file.textSync(),
    write: (contents) => file.write(contents),
    delete: () => file.delete(),
    moveTo: (dest) => file.moveTo(dest),
  });
  const openDirectory = store.openDirectory;
  store.openDirectory = (name): StoredDirectory => {
    const dir = openDirectory(name);
    return {
      uri: dir.uri,
      get exists() {
        return dir.exists;
      },
      create: () => dir.create(),
      list: () => dir.list().map(sized),
      openFile: (fileName) => sized(dir.openFile(fileName)),
    };
  };
  return store;
}

let store: MemoryFileStore;
let warn: jest.SpyInstance;

beforeEach(() => {
  store = createMemoryFileStore();
  setPinnedFileStore(store);
  usePinnedStore.setState({ entries: {}, queue: [], isWorking: false });
  fetchAudioUrlsMock.mockReset();
  fetchAudioUrlsMock.mockImplementation(async ([id]) => [resolved(id!)]);
  warn = jest.spyOn(console, 'warn').mockImplementation(() => {});
});

afterEach(() => {
  jest.useRealTimers();
  setPinnedFileStore();
  warn.mockRestore();
});

// A stalled transfer times out as a transient failure, so it is attempted three times: each
// spends the deadline, and the two backoffs in between wait at most 2s then 4s.
async function advanceThroughAttempts(): Promise<void> {
  for (let attempt = 0; attempt < 3; attempt += 1) {
    await jest.advanceTimersByTimeAsync(PIN_DOWNLOAD_TIMEOUT_MS);
    await flush();
    await jest.advanceTimersByTimeAsync(4_000);
    await flush();
  }
}

describe('a pinned download has its own deadline', () => {
  it('fails a stalled download once the deadline passes and moves the queue on to the next track', async () => {
    jest.useFakeTimers();
    const download = store.download;
    store.download = (url, dest, signal) =>
      url.includes('/stalled.mp3') ? new Promise<string>(() => {}) : download(url, dest, signal);

    const batch = usePinnedStore
      .getState()
      .pinMany([asTrackId('stalled'), asTrackId('next')]);
    await act(async () => flush());
    expect(usePinnedStore.getState().entries['stalled']?.status).toBe('downloading');
    expect(usePinnedStore.getState().entries['next']?.status).toBe('queued');

    await act(async () => {
      await advanceThroughAttempts();
    });

    expect(usePinnedStore.getState().entries['stalled']?.status).toBe('failed');
    expect(usePinnedStore.getState().entries['next']?.status).toBe('ready');
    expect(usePinnedStore.getState().isWorking).toBe(false);
    await expect(batch).resolves.toEqual({ requested: 2, failed: 1 });
  });

  it('aborts the stalled transfer and leaves no partial file behind to be adopted as ready', async () => {
    jest.useFakeTimers();
    let aborted = false;
    store.download = (_url, dest, signal) => {
      dest.write('partial');
      signal.addEventListener('abort', () => (aborted = true));
      return new Promise<string>(() => {});
    };

    void usePinnedStore.getState().pinMany([asTrackId('stalled')]);
    await act(async () => {
      await flush();
      await advanceThroughAttempts();
    });

    expect(aborted).toBe(true);
    expect(store.files.size).toBe(0);
    expect(usePinnedStore.getState().entries['stalled']?.status).toBe('failed');
  });
});

describe('a failed pinned download is logged', () => {
  it('logs the track id, the unsigned url and the caught error', async () => {
    jest.useFakeTimers();
    store.download = () => Promise.reject(new Error('disk full'));

    await act(async () => {
      const batch = usePinnedStore.getState().pinMany([asTrackId('t1')]);
      await flush();
      await jest.advanceTimersByTimeAsync(10_000);
      await batch;
    });

    expect(usePinnedStore.getState().entries['t1']?.status).toBe('failed');
    expect(warn).toHaveBeenCalledWith(expect.stringContaining('t1'), {
      url: 'https://cdn.example.com/audio/t1.mp3',
      error: expect.objectContaining({
        name: 'NetworkError',
        failure: 'transport',
        message: expect.stringContaining('disk full'),
      }),
    });
    expect(JSON.stringify(warn.mock.calls)).not.toContain('secret');
  });

  it('logs a failure to resolve the signed url with no url', async () => {
    const error = new Error('404');
    fetchAudioUrlsMock.mockRejectedValue(error);

    await act(async () => {
      await usePinnedStore.getState().pinMany([asTrackId('t1')]);
    });

    expect(warn).toHaveBeenCalledWith(expect.stringContaining('t1'), { url: undefined, error });
  });
});

describe('pinning is refused once pinned storage is full', () => {
  it('pinMany queues nothing and reports the refusal when pinned bytes reach the cap', async () => {
    withFileSize(store, MAX_PINNED_BYTES);
    store.files.set('memory://document/offline-audio/old.mp3', 'x');

    const result = await usePinnedStore.getState().pinMany([asTrackId('a'), asTrackId('b')]);

    expect(result).toEqual({ requested: 0, failed: 0, refused: 'storage-full' });
    expect(usePinnedStore.getState().entries).toEqual({});
    expect(usePinnedStore.getState().queue).toEqual([]);
    expect(fetchAudioUrlsMock).not.toHaveBeenCalled();
  });

  it('pin refuses when free disk space is below the reserve', () => {
    store.freeBytes = MIN_FREE_BYTES - 1;

    expect(usePinnedStore.getState().pin(asTrackId('a'))).toBe('storage-full');
    expect(usePinnedStore.getState().entries).toEqual({});
    expect(fetchAudioUrlsMock).not.toHaveBeenCalled();
  });

  it('pin accepts when there is room, and when the track needs no download', () => {
    expect(usePinnedStore.getState().pin(asTrackId('a'))).toBe('accepted');
    store.freeBytes = 0;
    expect(usePinnedStore.getState().pin(asTrackId('a'))).toBe('accepted');
  });

  it('does not block pinning when free disk space cannot be read', async () => {
    store.availableBytes = () => {
      throw new Error('unsupported');
    };

    await act(async () => {
      await expect(usePinnedStore.getState().pinMany([asTrackId('a')])).resolves.toEqual({
        requested: 1,
        failed: 0,
      });
    });
    expect(usePinnedStore.getState().entries['a']?.status).toBe('ready');
  });

  it('fails a queued track once the batch draining ahead of it has reached the byte cap', async () => {
    withFileSize(store, MAX_PINNED_BYTES / 2);

    await act(async () => {
      await expect(
        usePinnedStore.getState().pinMany([asTrackId('a'), asTrackId('b'), asTrackId('c')]),
      ).resolves.toEqual({ requested: 3, failed: 1 });
    });

    const { entries } = usePinnedStore.getState();
    expect(entries['b']?.status).toBe('ready');
    expect(entries['c']?.status).toBe('failed');
  });

  it('fails a queued track whose turn comes after storage filled up, without downloading it', async () => {
    let download!: () => void;
    fetchAudioUrlsMock.mockImplementationOnce(
      ([id]) =>
        new Promise<ResolvedAudioUrl[]>((res) => {
          download = () => res([resolved(id!)]);
        }),
    );

    const batch = usePinnedStore.getState().pinMany([asTrackId('a'), asTrackId('b')]);
    await act(async () => {
      await flush();
      store.freeBytes = 0;
      download();
      await flush();
    });

    await expect(batch).resolves.toEqual({ requested: 2, failed: 1 });
    expect(usePinnedStore.getState().entries['a']?.status).toBe('ready');
    expect(usePinnedStore.getState().entries['b']?.status).toBe('failed');
    expect(fetchAudioUrlsMock).toHaveBeenCalledTimes(1);
  });
});
