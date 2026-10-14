import {
  createMemoryFileStore,
  type MemoryFileStore,
} from '@shared/files/__tests__/memoryFileStore';

import {
  setAnalyticsPreferenceFileStore,
  shareAnalytics,
  useAnalyticsPreference,
} from '@shared/device/analyticsPreference';

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

  it('persists false across a reload', () => {
    useAnalyticsPreference.getState().setShareAnalytics(false);

    expect(store.files.get(PREF_URI)).toBe('false');
    setAnalyticsPreferenceFileStore(store);

    expect(shareAnalytics()).toBe(false);
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
