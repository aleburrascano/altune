import { asTrackId } from '@shared/api-client/ids';
import type { StoredDirectory, StoredFile } from '@shared/files/fileStore';
import {
  createMemoryFileStore,
  type MemoryFileStore,
} from '@shared/files/__tests__/memoryFileStore';

import {
  PIN_DOWNLOAD_TIMEOUT_MS,
  downloadPinned,
  findPinned,
  pinnedFilesByTrackId,
  setPinnedFileStore,
} from '../pinnedFiles';

const AUDIO_DIR = 'memory://document/offline-audio';
const T1 = asTrackId('t1');
const T1_URL = 'https://cdn.example.com/audio/t1.mp3?sig=secret';

let store: MemoryFileStore;
let warn: jest.SpyInstance;

beforeEach(() => {
  store = createMemoryFileStore();
  setPinnedFileStore(store);
  warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
});

afterEach(() => {
  jest.useRealTimers();
  warn.mockRestore();
  setPinnedFileStore();
});

function withFailingRename(target: MemoryFileStore): void {
  const openDirectory = target.openDirectory;
  target.openDirectory = (name): StoredDirectory => {
    const dir = openDirectory(name);
    const failingRename = (file: StoredFile): StoredFile => ({
      uri: file.uri,
      get exists() {
        return file.exists;
      },
      get size() {
        return file.size;
      },
      textSync: () => file.textSync(),
      write: (contents) => file.write(contents),
      delete: () => file.delete(),
      moveTo: () => {
        throw new Error('EXDEV: rename failed');
      },
    });
    return {
      uri: dir.uri,
      get exists() {
        return dir.exists;
      },
      create: () => dir.create(),
      list: () => dir.list(),
      openFile: (fileName) => failingRename(dir.openFile(fileName)),
    };
  };
}

describe('a pinned download in progress', () => {
  it('writes nowhere a track lookup would find it until the transfer completes', () => {
    let partialWritten = false;
    store.download = (_url, dest) => {
      dest.write('first-few-bytes');
      partialWritten = true;
      return new Promise<string>(() => {});
    };
    jest.useFakeTimers();

    void downloadPinned(T1, T1_URL);

    expect(partialWritten).toBe(true);
    expect(findPinned(T1)).toBeNull();
    expect(pinnedFilesByTrackId()?.has('t1')).toBe(false);
  });

  it('lands on the track name once complete, with nothing unfinished left beside it', async () => {
    const uri = await downloadPinned(T1, T1_URL);

    expect(uri).toBe(`${AUDIO_DIR}/t1.mp3`);
    expect([...store.files.keys()]).toEqual([`${AUDIO_DIR}/t1.mp3`]);
  });

  it('is never pinned when a rename onto the track name fails, and leaves no partial behind', async () => {
    withFailingRename(store);

    await expect(downloadPinned(T1, T1_URL)).rejects.toThrow('EXDEV: rename failed');

    expect(findPinned(T1)).toBeNull();
    expect([...store.files.keys()]).toEqual([]);
  });

  it('is never pinned by an adapter that keeps writing after its deadline expired', async () => {
    jest.useFakeTimers();
    store.download = (_url, dest) =>
      new Promise<string>((resolve) => {
        setTimeout(() => {
          dest.write('written-after-the-deadline');
          resolve(dest.uri);
        }, PIN_DOWNLOAD_TIMEOUT_MS * 2);
      });

    const download = downloadPinned(T1, T1_URL);
    const outcome = expect(download).rejects.toThrow('timed out');
    await jest.advanceTimersByTimeAsync(PIN_DOWNLOAD_TIMEOUT_MS * 2);
    await outcome;

    expect(findPinned(T1)).toBeNull();
  });
});
