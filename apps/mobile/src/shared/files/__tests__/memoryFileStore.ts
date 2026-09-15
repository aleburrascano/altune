import type { FileStore, StoredDirectory, StoredFile } from '../fileStore';

/**
 * An in-memory FileStore scoped to whoever creates it, so a test can give one module its own
 * filesystem without touching the suite-wide expo-file-system mock. `files`/`directories` are
 * exposed for seeding and assertions.
 */
export type MemoryFileStore = FileStore & {
  readonly files: Map<string, string>;
  readonly directories: Set<string>;
  /** What availableBytes() reports; defaults to plenty of room. */
  freeBytes: number;
};

const ROOT = 'memory://document';

export function createMemoryFileStore(): MemoryFileStore {
  const files = new Map<string, string>();
  const directories = new Set<string>();

  function openFileAt(uri: string): StoredFile {
    return {
      uri,
      get exists() {
        return files.has(uri);
      },
      get size() {
        return files.get(uri)?.length ?? null;
      },
      textSync() {
        const contents = files.get(uri);
        if (contents === undefined) throw new Error(`ENOENT: ${uri}`);
        return contents;
      },
      write(contents) {
        files.set(uri, contents);
      },
      delete() {
        files.delete(uri);
      },
    };
  }

  function openDirectoryAt(uri: string): StoredDirectory {
    return {
      uri,
      get exists() {
        return directories.has(uri);
      },
      create() {
        directories.add(uri);
      },
      list() {
        if (!directories.has(uri)) throw new Error(`ENOENT: ${uri}`);
        return [...files.keys()]
          .filter((path) => path.startsWith(`${uri}/`) && !path.slice(uri.length + 1).includes('/'))
          .map(openFileAt);
      },
      openFile: (name) => openFileAt(`${uri}/${name}`),
    };
  }

  const store: MemoryFileStore = {
    files,
    directories,
    freeBytes: 64 * 1024 ** 3,
    openDirectory: (name) => openDirectoryAt(`${ROOT}/${name}`),
    download: (url, dest, signal) => {
      if (signal.aborted) return Promise.reject(new Error('AbortError: download aborted'));
      files.set(dest.uri, `downloaded:${url}`);
      return Promise.resolve(dest.uri);
    },
    availableBytes: () => store.freeBytes,
  };
  return store;
}
