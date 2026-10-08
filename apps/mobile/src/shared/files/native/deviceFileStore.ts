import { Directory, File, Paths } from 'expo-file-system';

import type { FileStore, StoredDirectory, StoredFile } from '../fileStore';

export function fileHandle(file: File): StoredFile {
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
