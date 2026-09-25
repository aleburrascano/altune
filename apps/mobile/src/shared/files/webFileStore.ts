import type { FileStore, StoredDirectory, StoredFile } from './fileStore';

const KEY_PREFIX = 'altune:file:';
const ROOT = 'localstorage://document';
const CONSERVATIVE_AVAILABLE_BYTES = 5 * 1024 * 1024;

export class WebDownloadUnsupportedError extends Error {
  constructor() {
    super('download() is not supported on web');
    this.name = 'WebDownloadUnsupportedError';
  }
}

type FileCtx = { storage: Storage; key: string; uri: string };
type DirCtx = { storage: Storage; uri: string; path: string; marker: string };

function relativePath(uri: string): string {
  return uri.slice(ROOT.length + 1);
}

function fileKey(path: string): string {
  return `${KEY_PREFIX}${path}`;
}

function dirMarkerKey(path: string): string {
  return fileKey(`${path}/.dir`);
}

function fileSize(storage: Storage, key: string): number | null {
  const contents = storage.getItem(key);
  return contents === null ? null : contents.length;
}

function readFileText(storage: Storage, key: string, uri: string): string {
  const contents = storage.getItem(key);
  if (contents === null) throw new Error(`ENOENT: ${uri}`);
  return contents;
}

function moveFile(storage: Storage, sourceKey: string, destPath: string, uri: string): void {
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

function fileHandle(storage: Storage, uri: string): StoredFile {
  const ctx: FileCtx = { storage, key: fileKey(relativePath(uri)), uri };
  const handle = { uri, ...fileMethods(ctx) } as StoredFile;
  attachFileGetters(handle, ctx);
  return handle;
}

function namesDirectlyUnder(storage: Storage, path: string): readonly string[] {
  const prefix = fileKey(`${path}/`);
  const keys = Array.from({ length: storage.length }, (_, i) => storage.key(i));
  return keys
    .filter((key): key is string => key !== null && key.startsWith(prefix))
    .map((key) => key.slice(prefix.length))
    .filter((rest) => rest !== '.dir' && !rest.includes('/'));
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

function directoryHandle(storage: Storage, uri: string): StoredDirectory {
  const path = relativePath(uri);
  const ctx: DirCtx = { storage, uri, path, marker: dirMarkerKey(path) };
  const handle = { uri, ...dirMethods(ctx) } as StoredDirectory;
  attachDirGetter(handle, ctx);
  return handle;
}

export function createWebFileStore(storage: Storage): FileStore {
  return {
    openDirectory: (name) => directoryHandle(storage, `${ROOT}/${name}`),
    download: () => Promise.reject(new WebDownloadUnsupportedError()),
    availableBytes: () => CONSERVATIVE_AVAILABLE_BYTES,
  };
}

export const webFileStore: FileStore = createWebFileStore(globalThis.localStorage);
