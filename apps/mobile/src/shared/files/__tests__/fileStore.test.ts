import { deviceFileStore, type FileStore, createFileStoreSlot } from '../fileStore';
import { createMemoryFileStore } from './memoryFileStore';
import { createWebFileStore, WebDownloadUnsupportedError } from '../webFileStore';

function fakeLocalStorage(): Storage {
  const data = new Map<string, string>();
  return {
    getItem: (key) => data.get(key) ?? null,
    setItem: (key, value) => {
      data.set(key, value);
    },
    removeItem: (key) => {
      data.delete(key);
    },
    clear: () => data.clear(),
    key: (index) => [...data.keys()][index] ?? null,
    get length() {
      return data.size;
    },
  };
}

// The contract any FileStore must satisfy. The device store runs here against the suite-wide
// expo-file-system double; the in-memory store is the scoped fake tests inject into consumers;
// the web store runs against a fake localStorage (jest never resolves *.web.ts).
describe.each<[string, () => FileStore]>([
  ['deviceFileStore', () => deviceFileStore],
  ['createMemoryFileStore', createMemoryFileStore],
  ['createWebFileStore', () => createWebFileStore(fakeLocalStorage())],
])('%s satisfies the FileStore contract', (_name, makeStore) => {
  let store: FileStore;

  beforeEach(() => {
    store = makeStore();
  });

  it('opens a directory without creating it, and create() makes it exist', () => {
    const dir = store.openDirectory('contract');

    expect(dir.exists).toBe(false);
    dir.create();
    expect(dir.exists).toBe(true);
    expect(store.openDirectory('contract').exists).toBe(true);
  });

  it('list() throws for a directory that does not exist', () => {
    expect(() => store.openDirectory('missing').list()).toThrow();
  });

  it('opens a file without creating it; write, read, size and delete round-trip', () => {
    const dir = store.openDirectory('contract');
    dir.create();
    const file = dir.openFile('a.json');

    expect(file.uri).toBe(`${dir.uri}/a.json`);
    expect(file.exists).toBe(false);
    expect(file.size).toBeNull();
    expect(() => file.textSync()).toThrow();

    file.write('hello');
    expect(file.exists).toBe(true);
    expect(file.size).toBe(5);
    expect(dir.openFile('a.json').textSync()).toBe('hello');

    file.write('replaced');
    expect(file.textSync()).toBe('replaced');

    file.delete();
    expect(file.exists).toBe(false);
    expect(file.size).toBeNull();
  });

  it('moveTo() renames a file onto another, replacing what was there', () => {
    const dir = store.openDirectory('contract');
    dir.create();
    const temp = dir.openFile('a.json.tmp');
    temp.write('new');
    dir.openFile('a.json').write('old');

    temp.moveTo(dir.openFile('a.json'));

    expect(dir.openFile('a.json').textSync()).toBe('new');
    expect(dir.openFile('a.json.tmp').exists).toBe(false);
  });

  it('moveTo() onto a file that does not exist yet creates it', () => {
    const dir = store.openDirectory('contract');
    dir.create();
    dir.openFile('a.json.tmp').write('new');

    dir.openFile('a.json.tmp').moveTo(dir.openFile('a.json'));

    expect(dir.openFile('a.json').textSync()).toBe('new');
  });

  it('moveTo() throws for a source file that does not exist', () => {
    const dir = store.openDirectory('contract');
    dir.create();

    expect(() => dir.openFile('missing').moveTo(dir.openFile('a.json'))).toThrow();
  });

  it('list() returns only the files directly inside the directory', () => {
    const dir = store.openDirectory('contract');
    dir.create();
    dir.openFile('a.mp3').write('a');
    dir.openFile('b.mp3').write('b');
    const other = store.openDirectory('contract-other');
    other.create();
    other.openFile('c.mp3').write('c');
    const nested = store.openDirectory('contract/nested');
    nested.create();
    nested.openFile('d.mp3').write('d');

    expect(
      dir
        .list()
        .map((f) => f.uri)
        .sort(),
    ).toEqual([`${dir.uri}/a.mp3`, `${dir.uri}/b.mp3`]);
  });

  it('availableBytes() reports a non-negative byte count', () => {
    const bytes = store.availableBytes();

    expect(Number.isFinite(bytes)).toBe(true);
    expect(bytes).toBeGreaterThanOrEqual(0);
  });
});

describe.each<[string, () => FileStore]>([
  ['deviceFileStore', () => deviceFileStore],
  ['createMemoryFileStore', createMemoryFileStore],
])('%s download() actually downloads', (_name, makeStore) => {
  let store: FileStore;

  beforeEach(() => {
    store = makeStore();
  });

  it('download() writes into the destination file and resolves to its uri', async () => {
    const dir = store.openDirectory('contract');
    dir.create();
    const dest = dir.openFile('t1.mp3');

    await expect(
      store.download('https://cdn.example.com/t1.mp3', dest, new AbortController().signal),
    ).resolves.toBe(dest.uri);
    expect(dest.exists).toBe(true);
  });

  it('download() rejects without writing when its signal is already aborted', async () => {
    const dir = store.openDirectory('contract');
    dir.create();
    const dest = dir.openFile('t1.mp3');
    const controller = new AbortController();
    controller.abort();

    await expect(
      store.download('https://cdn.example.com/t1.mp3', dest, controller.signal),
    ).rejects.toThrow();
    expect(dest.exists).toBe(false);
  });
});

describe('createWebFileStore download() is unsupported', () => {
  it('download() rejects with WebDownloadUnsupportedError and writes nothing', async () => {
    const store = createWebFileStore(fakeLocalStorage());
    const dir = store.openDirectory('contract');
    dir.create();
    const dest = dir.openFile('t1.mp3');

    await expect(
      store.download('https://cdn.example.com/t1.mp3', dest, new AbortController().signal),
    ).rejects.toBeInstanceOf(WebDownloadUnsupportedError);
    expect(dest.exists).toBe(false);
  });
});

describe('createFileStoreSlot', () => {
  // The seam every persisted store binds through: killSwitch, pinnedFiles, pinnedIndex and
  // outboxStore each hold one slot, and their test suites swap and restore it around every case.
  describe('a file store slot', () => {
    it('starts bound to the device filesystem when created with no default', () => {
      const slot = createFileStoreSlot();

      expect(slot.get()).toBe(deviceFileStore);
    });

    it('ensureDir() creates a directory that is not there yet', () => {
      const store = createMemoryFileStore();
      const slot = createFileStoreSlot(store);

      const dir = slot.ensureDir('outbox');

      expect(dir.exists).toBe(true);
      expect(store.openDirectory('outbox').exists).toBe(true);
    });

    it('ensureDir() keeps what a directory that already exists holds', () => {
      const slot = createFileStoreSlot(createMemoryFileStore());
      slot.ensureDir('outbox').openFile('a.json').write('kept');

      const dir = slot.ensureDir('outbox');

      expect(dir.openFile('a.json').textSync()).toBe('kept');
    });

    it('writes through the store it was last set to, not the one it replaced', () => {
      const replaced = createMemoryFileStore();
      const slot = createFileStoreSlot(replaced);
      const store = createMemoryFileStore();

      slot.set(store);
      slot.ensureDir('outbox').openFile('a.json').write('written');

      expect(store.openDirectory('outbox').openFile('a.json').textSync()).toBe('written');
      expect(replaced.openDirectory('outbox').exists).toBe(false);
    });

    it('set() with no argument binds the default back', () => {
      const fallback = createMemoryFileStore();
      const slot = createFileStoreSlot(fallback);
      slot.set(createMemoryFileStore());

      slot.set();

      expect(slot.get()).toBe(fallback);
    });
  });
});
