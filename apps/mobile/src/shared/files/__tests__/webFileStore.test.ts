import type { FileStore } from '../fileStore';
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

describe('createWebFileStore', () => {
  it('opens a directory without creating it, and create() makes it exist', () => {
    const store = createWebFileStore(fakeLocalStorage());
    const dir = store.openDirectory('contract');

    expect(dir.exists).toBe(false);
    dir.create();
    expect(dir.exists).toBe(true);
    expect(store.openDirectory('contract').exists).toBe(true);
  });

  it('list() throws for a directory that does not exist', () => {
    const store = createWebFileStore(fakeLocalStorage());

    expect(() => store.openDirectory('missing').list()).toThrow();
  });

  it('opens a file without creating it; write, read, size and delete round-trip', () => {
    const store = createWebFileStore(fakeLocalStorage());
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
    const store = createWebFileStore(fakeLocalStorage());
    const dir = store.openDirectory('contract');
    dir.create();
    const temp = dir.openFile('a.json.tmp');
    temp.write('new');
    dir.openFile('a.json').write('old');

    temp.moveTo(dir.openFile('a.json'));

    expect(dir.openFile('a.json').textSync()).toBe('new');
    expect(dir.openFile('a.json.tmp').exists).toBe(false);
  });

  it('moveTo() throws for a source file that does not exist', () => {
    const store = createWebFileStore(fakeLocalStorage());
    const dir = store.openDirectory('contract');
    dir.create();

    expect(() => dir.openFile('missing').moveTo(dir.openFile('a.json'))).toThrow();
  });

  it('list() returns only the files directly inside the directory', () => {
    const store = createWebFileStore(fakeLocalStorage());
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

  it('keys every write under the altune:file: prefix so it never collides with unrelated storage', () => {
    const storage = fakeLocalStorage();
    const store = createWebFileStore(storage);
    const dir = store.openDirectory('kill-switch');
    dir.create();
    dir.openFile('switches.json').write('{}');

    expect(storage.getItem('altune:file:kill-switch/switches.json')).toBe('{}');
  });

  it('persists a write made through one store into a second store over the same storage', () => {
    const storage = fakeLocalStorage();
    createWebFileStore(storage).openDirectory('preferences').create();
    createWebFileStore(storage).openDirectory('preferences').openFile('theme.json').write('"dark"');

    const reopened = createWebFileStore(storage)
      .openDirectory('preferences')
      .openFile('theme.json');

    expect(reopened.textSync()).toBe('"dark"');
  });

  it('download() rejects with a typed unsupported error and writes nothing', async () => {
    const store = createWebFileStore(fakeLocalStorage());
    const dir = store.openDirectory('contract');
    dir.create();
    const dest = dir.openFile('t1.mp3');

    await expect(
      store.download('https://cdn.example.com/t1.mp3', dest, new AbortController().signal),
    ).rejects.toBeInstanceOf(WebDownloadUnsupportedError);
    expect(dest.exists).toBe(false);
  });

  it('availableBytes() reports a non-negative byte count', () => {
    const store = createWebFileStore(fakeLocalStorage());

    const bytes = store.availableBytes();

    expect(Number.isFinite(bytes)).toBe(true);
    expect(bytes).toBeGreaterThanOrEqual(0);
  });
});

describe('the webFileStore singleton', () => {
  async function withThrowingLocalStorage<T>(run: (getter: jest.Mock) => Promise<T> | T): Promise<T> {
    const getter = jest.fn(() => {
      throw new DOMException('blocked', 'SecurityError');
    });
    Object.defineProperty(globalThis, 'localStorage', { configurable: true, get: getter });
    try {
      return await run(getter);
    } finally {
      delete (globalThis as { localStorage?: Storage }).localStorage;
    }
  }

  it('never touches localStorage while the module is imported', async () => {
    await withThrowingLocalStorage((getter) => {
      jest.isolateModules(() => {
        require('../webFileStore');
      });
      expect(getter).not.toHaveBeenCalled();
    });
  });

  it('degrades to an in-memory store instead of throwing when localStorage is unreachable', async () => {
    await withThrowingLocalStorage(async () => {
      let webFileStore!: FileStore;
      let IsolatedUnsupportedError!: typeof WebDownloadUnsupportedError;
      jest.isolateModules(() => {
        const isolated = require('../webFileStore');
        webFileStore = isolated.webFileStore;
        IsolatedUnsupportedError = isolated.WebDownloadUnsupportedError;
      });

      const dir = webFileStore.openDirectory('contract');
      expect(dir.exists).toBe(false);
      dir.create();
      expect(dir.exists).toBe(true);
      const file = dir.openFile('a.json');
      file.write('hello');
      expect(file.textSync()).toBe('hello');
      expect(dir.list().map((f) => f.uri)).toEqual([file.uri]);
      file.delete();
      expect(file.exists).toBe(false);

      expect(webFileStore.availableBytes()).toBeGreaterThan(0);
      await expect(
        webFileStore.download('https://cdn.example.com/a.mp3', file, new AbortController().signal),
      ).rejects.toBeInstanceOf(IsolatedUnsupportedError);
    });
  });

  it('falls back to an in-memory store when there is no localStorage at all, without throwing', () => {
    let webFileStore!: FileStore;
    jest.isolateModules(() => {
      ({ webFileStore } = require('../webFileStore'));
    });

    const dir = webFileStore.openDirectory('contract');
    dir.create();

    expect(dir.exists).toBe(true);
  });

  it('reaches real localStorage when it is available', () => {
    const storage = fakeLocalStorage();
    Object.defineProperty(globalThis, 'localStorage', { configurable: true, value: storage });
    try {
      let webFileStore!: FileStore;
      jest.isolateModules(() => {
        ({ webFileStore } = require('../webFileStore'));
      });

      webFileStore.openDirectory('contract').create();

      expect(storage.getItem('altune:file:contract/.dir')).toBe('1');
    } finally {
      delete (globalThis as { localStorage?: Storage }).localStorage;
    }
  });
});
