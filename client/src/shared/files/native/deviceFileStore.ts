import { Directory, File, Paths } from 'expo-file-system';

import type { FileStore, StoredDirectory, StoredFile } from '@shared/files/fileStore';

function fileSize(file: File): number | null {
  return file.exists ? file.size : null;
}

function fileState(file: File): Pick<StoredFile, 'exists' | 'size'> {
  return {
    get exists() {
      return file.exists;
    },
    get size() {
      return fileSize(file);
    },
  };
}

function fileOperations(file: File): Omit<StoredFile, 'exists' | 'size'> {
  return {
    uri: file.uri,
    textSync: () => file.textSync(),
    write: (contents) => file.write(contents),
    delete: () => file.delete(),
    moveTo: (dest) => file.moveSync(new File(dest.uri), { overwrite: true }),
  };
}

export function fileHandle(file: File): StoredFile {
  const state = Object.getOwnPropertyDescriptors(fileState(file));
  return Object.defineProperties({ ...fileOperations(file) }, state) as StoredFile;
}

function listFiles(dir: Directory): StoredFile[] {
  return dir
    .list()
    .filter((entry): entry is File => entry instanceof File)
    .map(fileHandle);
}

function directoryState(dir: Directory): Pick<StoredDirectory, 'uri' | 'exists'> {
  return {
    uri: dir.uri,
    get exists() {
      return dir.exists;
    },
  };
}

function directoryOperations(dir: Directory): Omit<StoredDirectory, 'uri' | 'exists'> {
  return {
    create: () => dir.create({ intermediates: true }),
    list: () => listFiles(dir),
    openFile: (name) => fileHandle(new File(dir, name)),
  };
}

function directoryHandle(dir: Directory): StoredDirectory {
  const state = Object.getOwnPropertyDescriptors(directoryState(dir));
  return Object.defineProperties({ ...directoryOperations(dir) }, state) as StoredDirectory;
}

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
