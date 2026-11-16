import type { FileStore } from '@shared/files';

import { openWebFileStore } from '@/mobile';

export function fakeLocalStorage(): Storage {
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

export const WEB_DOWNLOAD_UNSUPPORTED = {
  name: 'WebDownloadUnsupportedError',
  message: 'download() is not supported on web',
};

export function webFileStoreOver(storage: Storage): FileStore {
  Object.defineProperty(globalThis, 'localStorage', { configurable: true, value: storage });
  try {
    const store = openWebFileStore();
    store.availableBytes();
    return store;
  } finally {
    delete (globalThis as { localStorage?: Storage }).localStorage;
  }
}
