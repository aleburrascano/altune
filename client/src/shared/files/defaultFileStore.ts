import type { FileStore } from '@shared/files/fileStore';

let deviceFileStore: FileStore | undefined;

function storeTheAppSet(): FileStore {
  if (deviceFileStore === undefined) {
    throw new Error('[files] no device file store: the app sets one before anything reads a file');
  }
  return deviceFileStore;
}

export const defaultFileStore: FileStore = {
  openDirectory: (name) => storeTheAppSet().openDirectory(name),
  download: (...args) => storeTheAppSet().download(...args),
  availableBytes: () => storeTheAppSet().availableBytes(),
};

export function setDeviceFileStore(store: FileStore): void {
  deviceFileStore = store;
}
