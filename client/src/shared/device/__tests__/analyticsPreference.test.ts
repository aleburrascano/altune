import { createMemoryFileStore, type MemoryFileStore } from '@shared/files';

import {
  setAnalyticsPreferenceFileStore,
  shareAnalytics,
  useAnalyticsPreference,
} from '@shared/device';

const PREF_URI = 'memory://document/preferences/analytics.json';
const DISK_FULL = new Error('ENOSPC: no space left on device');

function storeThatRefusesWrites(): MemoryFileStore {
  const store = createMemoryFileStore();
  return {
    ...store,
    openDirectory: (name) => {
      const directory = store.openDirectory(name);
      return {
        ...directory,
        openFile: (fileName) => ({
          ...directory.openFile(fileName),
          write: () => {
            throw DISK_FULL;
          },
        }),
      };
    },
  };
}

function storeWhereWritesNeedTheFolderAndCreatingItTwiceThrows(): MemoryFileStore {
  const store = createMemoryFileStore();
  return {
    ...store,
    openDirectory: (name) => {
      const directory = store.openDirectory(name);
      return {
        ...directory,
        get exists() {
          return directory.exists;
        },
        create: () => {
          if (directory.exists) throw new Error(`EEXIST: ${directory.uri}`);
          directory.create();
        },
        openFile: (fileName) => ({
          ...directory.openFile(fileName),
          write: (contents) => {
            if (!directory.exists) throw new Error(`ENOENT: ${directory.uri}`);
            directory.openFile(fileName).write(contents);
          },
        }),
      };
    },
  };
}

type CountedStore = { store: MemoryFileStore; reads: () => number };

function storeThatCountsReads(): CountedStore {
  const store = createMemoryFileStore();
  let reads = 0;
  const counted: MemoryFileStore = {
    ...store,
    openDirectory: (name) => {
      const directory = store.openDirectory(name);
      return {
        ...directory,
        openFile: (fileName) => {
          const file = directory.openFile(fileName);
          return {
            ...file,
            textSync: () => {
              reads += 1;
              return file.textSync();
            },
          };
        },
      };
    },
  };
  return { store: counted, reads: () => reads };
}

let store: MemoryFileStore;
let warnSpy: jest.SpyInstance;

beforeEach(() => {
  store = createMemoryFileStore();
  setAnalyticsPreferenceFileStore(store);
  warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => {});
});

afterEach(() => {
  setAnalyticsPreferenceFileStore();
  warnSpy.mockRestore();
});

describe('analytics preference', () => {
  it('shares analytics when nothing was ever saved', () => {
    expect(useAnalyticsPreference.getState().shareAnalytics).toBe(true);
    expect(shareAnalytics()).toBe(true);
  });

  it('does not try to read a preference file that was never saved', () => {
    const counted = storeThatCountsReads();

    setAnalyticsPreferenceFileStore(counted.store);

    expect(shareAnalytics()).toBe(true);
    expect(counted.reads()).toBe(0);
  });

  it('persists false across a reload', () => {
    useAnalyticsPreference.getState().setShareAnalytics(false);

    expect(store.files.get(PREF_URI)).toBe('false');
    setAnalyticsPreferenceFileStore(store);

    expect(shareAnalytics()).toBe(false);
  });

  it('replaces the in-memory choice with the one the new device store holds', () => {
    useAnalyticsPreference.getState().setShareAnalytics(true);
    const device = createMemoryFileStore();
    device.files.set(PREF_URI, 'false');

    setAnalyticsPreferenceFileStore(device);
    expect(shareAnalytics()).toBe(false);

    setAnalyticsPreferenceFileStore(createMemoryFileStore());
    expect(shareAnalytics()).toBe(true);
  });

  it('persists turning sharing back on across a reload', () => {
    useAnalyticsPreference.getState().setShareAnalytics(false);
    useAnalyticsPreference.getState().setShareAnalytics(true);

    setAnalyticsPreferenceFileStore(store);

    expect(shareAnalytics()).toBe(true);
  });

  it('creates the preferences folder once, then saves into it on later changes', () => {
    const device = storeWhereWritesNeedTheFolderAndCreatingItTwiceThrows();
    setAnalyticsPreferenceFileStore(device);

    useAnalyticsPreference.getState().setShareAnalytics(false);
    setAnalyticsPreferenceFileStore(device);
    expect(shareAnalytics()).toBe(false);

    useAnalyticsPreference.getState().setShareAnalytics(true);
    setAnalyticsPreferenceFileStore(device);
    expect(shareAnalytics()).toBe(true);
    expect(warnSpy).not.toHaveBeenCalled();
  });

  it('falls back to sharing when the saved file is unreadable', () => {
    store.files.set(PREF_URI, '{not json');

    setAnalyticsPreferenceFileStore(store);

    expect(shareAnalytics()).toBe(true);
  });

  it('keeps the in-memory choice and warns when the write fails', () => {
    setAnalyticsPreferenceFileStore(storeThatRefusesWrites());

    useAnalyticsPreference.getState().setShareAnalytics(false);

    expect(shareAnalytics()).toBe(false);
    expect(warnSpy).toHaveBeenCalledWith(expect.stringContaining('false'), DISK_FULL);
  });
});
