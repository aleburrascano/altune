import type { FileStore, StoredDirectory, StoredFile } from '@shared/files/fileStore';

const KEY_PREFIX = 'altune:file:';
const ROOT = 'localstorage://document';
const CONSERVATIVE_AVAILABLE_BYTES = 5 * 1024 * 1024;

type KeyValueStore = {
  readonly length: number;
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
  removeItem(key: string): void;
  key(index: number): string | null | undefined;
};

class WebDownloadUnsupportedError extends Error {
  constructor() {
    super('download() is not supported on web');
    this.name = 'WebDownloadUnsupportedError';
  }
}

type FileCtx = { storage: KeyValueStore; key: string; uri: string };
type DirCtx = { storage: KeyValueStore; uri: string; path: string; marker: string };

function relativePath(uri: string): string {
  return uri.slice(ROOT.length + 1);
}

function fileKey(path: string): string {
  return `${KEY_PREFIX}${path}`;
}

function dirMarkerKey(path: string): string {
  return fileKey(`${path}/.dir`);
}

function fileSize(storage: KeyValueStore, key: string): number | null {
  const contents = storage.getItem(key);
  return contents === null ? null : contents.length;
}

function readFileText(storage: KeyValueStore, key: string, uri: string): string {
  const contents = storage.getItem(key);
  if (contents === null) throw new Error(`ENOENT: ${uri}`);
  return contents;
}

function moveFile(storage: KeyValueStore, sourceKey: string, destPath: string, uri: string): void {
  const contents = storage.getItem(sourceKey);
  if (contents === null) throw new Error(`ENOENT: ${uri}`);
  storage.setItem(fileKey(destPath), contents);
  storage.removeItem(sourceKey);
}

function attachFileGetters(target: object, { storage, key }: FileCtx): void {
  Object.defineProperty(target, 'exists', {
    get: () => storage.getItem(key) !== null,
    enumerable: true,
  });
  Object.defineProperty(target, 'size', { get: () => fileSize(storage, key), enumerable: true });
}

function fileMethods({ storage, key, uri }: FileCtx) {
  return {
    textSync: () => readFileText(storage, key, uri),
    write: (contents: string) => storage.setItem(key, contents),
    delete: () => storage.removeItem(key),
    moveTo: (dest: StoredFile) => moveFile(storage, key, relativePath(dest.uri), uri),
  };
}

function fileHandle(storage: KeyValueStore, uri: string): StoredFile {
  const ctx: FileCtx = { storage, key: fileKey(relativePath(uri)), uri };
  const handle = { uri, ...fileMethods(ctx) } as StoredFile;
  attachFileGetters(handle, ctx);
  return handle;
}

function directChildName(key: string, prefix: string): string | null {
  if (!key.startsWith(prefix)) return null;
  const rest = key.slice(prefix.length);
  return rest !== '.dir' && !rest.includes('/') ? rest : null;
}

function namesDirectlyUnder(storage: KeyValueStore, path: string): readonly string[] {
  const prefix = fileKey(`${path}/`);
  const keys = Array.from({ length: storage.length }, (_, i) => storage.key(i) as string);
  const names: string[] = [];
  for (const key of keys) {
    const name = directChildName(key, prefix);
    if (name !== null) names.push(name);
  }
  return names;
}

function attachDirGetter(target: object, { storage, marker }: DirCtx): void {
  Object.defineProperty(target, 'exists', {
    get: () => storage.getItem(marker) !== null,
    enumerable: true,
  });
}

function listFiles({ storage, uri, path, marker }: DirCtx): readonly StoredFile[] {
  if (storage.getItem(marker) === null) throw new Error(`ENOENT: ${uri}`);
  return namesDirectlyUnder(storage, path).map((name) => fileHandle(storage, `${uri}/${name}`));
}

function dirMethods(ctx: DirCtx) {
  return {
    create: () => ctx.storage.setItem(ctx.marker, '1'),
    list: () => listFiles(ctx),
    openFile: (name: string) => fileHandle(ctx.storage, `${ctx.uri}/${name}`),
  };
}

function directoryHandle(storage: KeyValueStore, uri: string): StoredDirectory {
  const path = relativePath(uri);
  const ctx: DirCtx = { storage, uri, path, marker: dirMarkerKey(path) };
  const handle = { uri, ...dirMethods(ctx) } as StoredDirectory;
  attachDirGetter(handle, ctx);
  return handle;
}

function createWebFileStore(storage: KeyValueStore): FileStore {
  return {
    openDirectory: (name) => directoryHandle(storage, `${ROOT}/${name}`),
    download: () => Promise.reject(new WebDownloadUnsupportedError()),
    availableBytes: () => CONSERVATIVE_AVAILABLE_BYTES,
  };
}

class InMemoryStore implements KeyValueStore {
  private readonly entries = new Map<string, string>();

  get length(): number {
    return this.entries.size;
  }

  getItem(key: string): string | null {
    return this.entries.get(key) ?? null;
  }

  setItem(key: string, value: string): void {
    this.entries.set(key, value);
  }

  removeItem(key: string): void {
    this.entries.delete(key);
  }

  key(index: number): string | undefined {
    return [...this.entries.keys()][index];
  }
}

function resolveBrowserOrFallbackStorage(): KeyValueStore {
  try {
    return typeof window !== 'undefined' && window.localStorage
      ? window.localStorage
      : new InMemoryStore();
  } catch {
    return new InMemoryStore();
  }
}

let lazilyCreatedWebFileStore: FileStore | undefined;

function currentStore(): FileStore {
  if (!lazilyCreatedWebFileStore) {
    lazilyCreatedWebFileStore = createWebFileStore(resolveBrowserOrFallbackStorage());
  }
  return lazilyCreatedWebFileStore;
}

export const webFileStore: FileStore = {
  openDirectory: (name) => currentStore().openDirectory(name),
  download: (url, dest, signal) => currentStore().download(url, dest, signal),
  availableBytes: () => currentStore().availableBytes(),
};
