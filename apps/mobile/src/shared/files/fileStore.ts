import { Directory, File, Paths } from 'expo-file-system';
import { Platform } from 'react-native';

import { webFileStore } from './webFileStore';

export type StoredFile = {
  readonly uri: string;
  readonly exists: boolean;
  readonly size: number | null;
  textSync(): string;
  write(contents: string): void;
  delete(): void;
  moveTo(dest: StoredFile): void;
};

export type StoredDirectory = {
  readonly uri: string;
  readonly exists: boolean;
  create(): void;
  list(): readonly StoredFile[];
  openFile(name: string): StoredFile;
};

export type FileStore = {
  openDirectory(name: string): StoredDirectory;
  download(
    url: string,
    dest: StoredFile,
    signal: AbortSignal,
    onProgress?: (progress: { bytesWritten: number; totalBytes: number }) => void,
  ): Promise<string>;
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

export { fileHandle };

export const deviceFileStore: FileStore = {
  openDirectory: (name) => directoryHandle(new Directory(Paths.document, name)),
  download: (url, dest, signal, onProgress) =>
    File.downloadFileAsync(url, new File(dest.uri), {
      idempotent: true,
      signal,
      ...(onProgress === undefined ? {} : { onProgress }),
    }).then((file) => file.uri),
  availableBytes: () => Paths.availableDiskSpace,
};

export const defaultFileStore: FileStore = Platform.OS === 'web' ? webFileStore : deviceFileStore;

export type FileStoreSlot = {
  get(): FileStore;
  set(store?: FileStore): void;
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
