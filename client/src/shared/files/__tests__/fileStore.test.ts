import { type FileStore, createFileStoreSlot, createMemoryFileStore } from '@shared/files';

function onProgress(): void {}

type FilesDoor = {
  defaultFileStore: FileStore;
  setDeviceFileStore(store: FileStore): void;
};

function filesBeforeTheAppSetsAStore(): FilesDoor {
  let files!: FilesDoor;
  jest.isolateModules(() => {
    files = jest.requireActual('@shared/files');
  });
  return files;
}

const NO_STORE = /no device file store/;
const FREE_BYTES = 1234;

describe('createFileStoreSlot', () => {
  describe('a file store slot', () => {
    it('starts bound to the default it was created with', () => {
      const store = createMemoryFileStore();
      const slot = createFileStoreSlot(store);

      expect(slot.get()).toBe(store);
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

describe('the default file store', () => {
  it('refuses every call until the app sets the device file store', () => {
    const { defaultFileStore } = filesBeforeTheAppSetsAStore();
    const dest = createMemoryFileStore().openDirectory('a').openFile('t1.mp3');

    expect(() => defaultFileStore.openDirectory('preferences')).toThrow(NO_STORE);
    expect(() => defaultFileStore.availableBytes()).toThrow(NO_STORE);
    expect(() =>
      defaultFileStore.download(
        'https://cdn.example.com/t1.mp3',
        dest,
        new AbortController().signal,
      ),
    ).toThrow(NO_STORE);
  });

  it('reads and writes the device file store the app set', () => {
    const { defaultFileStore, setDeviceFileStore } = filesBeforeTheAppSetsAStore();
    const device = createMemoryFileStore();
    device.freeBytes = FREE_BYTES;

    setDeviceFileStore(device);
    defaultFileStore.openDirectory('preferences').create();

    expect(device.openDirectory('preferences').exists).toBe(true);
    expect(defaultFileStore.availableBytes()).toBe(FREE_BYTES);
  });

  it('hands download() every argument, progress listener included', async () => {
    const { defaultFileStore, setDeviceFileStore } = filesBeforeTheAppSetsAStore();
    const calls: Parameters<FileStore['download']>[] = [];
    setDeviceFileStore({
      ...createMemoryFileStore(),
      download: (...args) => {
        calls.push(args);
        return Promise.resolve('file:///t1.mp3');
      },
    });
    const dest = createMemoryFileStore().openDirectory('a').openFile('t1.mp3');
    const { signal } = new AbortController();

    await expect(
      defaultFileStore.download('https://cdn.example.com/t1.mp3', dest, signal, onProgress),
    ).resolves.toBe('file:///t1.mp3');

    expect(calls).toEqual([['https://cdn.example.com/t1.mp3', dest, signal, onProgress]]);
  });

  it('follows the store set last', () => {
    const { defaultFileStore, setDeviceFileStore } = filesBeforeTheAppSetsAStore();
    const first = createMemoryFileStore();
    const second = createMemoryFileStore();

    setDeviceFileStore(first);
    setDeviceFileStore(second);
    defaultFileStore.openDirectory('preferences').create();

    expect(second.openDirectory('preferences').exists).toBe(true);
    expect(first.openDirectory('preferences').exists).toBe(false);
  });
});
