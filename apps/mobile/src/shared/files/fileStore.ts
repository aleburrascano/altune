import { Directory, File, Paths } from 'expo-file-system';
import { Platform } from 'react-native';

import { webFileStore } from './webFileStore';

// The filesystem port the persisted stores (pinned audio + index, telemetry outbox) write through.
// Each consumer holds one `createFileStoreSlot()`, bound to `deviceFileStore` until its setter
// swaps it, so one test can hand one module a scoped fake instead of reconfiguring the suite-wide
// expo-file-system jest mock.
// The contract every implementation must satisfy is pinned by __tests__/fileStore.contract.test.ts.

/** A file handle. Opening one never touches the disk; the file need not exist yet. */
export type StoredFile = {
  readonly uri: string;
  readonly exists: boolean;
  /** Byte size, or null when the file does not exist. */
  readonly size: number | null;
  /** Throws when the file does not exist or cannot be read. */
  textSync(): string;
  /** Creates or replaces the file's contents. */
  write(contents: string): void;
  delete(): void;
  /**
   * Renames this file onto `dest`, replacing any file already there. Where the platform allows it
   * this is one atomic rename, so a reader sees the old or the new file, never a partial one. The
   * handle is spent afterwards: open `dest` to keep using the file.
   */
  moveTo(dest: StoredFile): void;
};

/** A directory handle. Opening one never touches the disk; the directory need not exist yet. */
export type StoredDirectory = {
  readonly uri: string;
  readonly exists: boolean;
  /** Creates the directory and any missing parents. */
  create(): void;
  /** The files directly inside the directory (subdirectories are skipped); throws if unreadable. */
  list(): readonly StoredFile[];
  openFile(name: string): StoredFile;
};

export type FileStore = {
  /** Opens a directory under the app's document root, which the OS never evicts. */
  openDirectory(name: string): StoredDirectory;
  /**
   * Downloads `url` into `dest`, replacing any existing file; resolves to the written file's uri.
   * Aborting `signal` cancels the transfer and rejects.
   */
  download(url: string, dest: StoredFile, signal: AbortSignal): Promise<string>;
  /** Free bytes on the device's internal storage; may throw where the platform cannot report it. */
  availableBytes(): number;
};

function fileHandle(file: File): StoredFile {
  return {
    uri: file.uri,
    get exists() {
      return file.exists;
    },
    get size() {
      return file.exists ? file.size : null;
    },
    textSync: () => file.textSync(),
    write: (contents) => file.write(contents),
    delete: () => file.delete(),
    moveTo: (dest) => file.moveSync(new File(dest.uri), { overwrite: true }),
  };
}

function directoryHandle(dir: Directory): StoredDirectory {
  return {
    uri: dir.uri,
    get exists() {
      return dir.exists;
    },
    create: () => dir.create({ intermediates: true }),
    list: () =>
      dir
        .list()
        .filter((entry): entry is File => entry instanceof File)
        .map(fileHandle),
    openFile: (name) => fileHandle(new File(dir, name)),
  };
}

/** The real on-device filesystem, backed by expo-file-system. */
export const deviceFileStore: FileStore = {
  openDirectory: (name) => directoryHandle(new Directory(Paths.document, name)),
  download: async (url, dest, signal) => {
    const file = await File.downloadFileAsync(url, new File(dest.uri), { idempotent: true, signal });
    return file.uri;
  },
  availableBytes: () => Paths.availableDiskSpace,
};

export const defaultFileStore: FileStore = Platform.OS === 'web' ? webFileStore : deviceFileStore;

/** One module's binding of the store it persists through, swappable by that module's test setter. */
export type FileStoreSlot = {
  get(): FileStore;
  /** Binds `store`; with no argument, back at the slot's default. */
  set(store?: FileStore): void;
  /** Opens a directory under the document root, creating it when it is not there yet. */
  ensureDir(name: string): StoredDirectory;
};

export function createFileStoreSlot(defaultStore: FileStore = deviceFileStore): FileStoreSlot {
  let store = defaultStore;
  return {
    get: () => store,
    set: (next = defaultStore) => {
      store = next;
    },
    ensureDir: (name) => {
      const dir = store.openDirectory(name);
      if (!dir.exists) dir.create();
      return dir;
    },
  };
}
