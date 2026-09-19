import {
  createMemoryFileStore,
  type MemoryFileStore,
} from '@shared/files/__tests__/memoryFileStore';

import { setThemePreferenceFileStore, useThemePreference } from '../themePreference';

const PREF_URI = 'memory://document/preferences/theme.json';
const LIGHT_ON_DISK = '"light"';
const DISK_FULL = new Error('ENOSPC: no space left on device');

/** A store that reads like any other but refuses every write, the shape a full disk takes. */
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
  setThemePreferenceFileStore(store);
  warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => {});
});

afterEach(() => {
  setThemePreferenceFileStore();
  warnSpy.mockRestore();
});

describe('theme preference', () => {
  it('starts dark when nothing was ever saved', () => {
    expect(useThemePreference.getState().scheme).toBe('dark');
  });

  it('starts on the scheme saved in the file store it is pointed at', () => {
    store.files.set(PREF_URI, LIGHT_ON_DISK);

    setThemePreferenceFileStore(store);

    expect(useThemePreference.getState().scheme).toBe('light');
  });

  it('saves the chosen scheme through the file store it is pointed at', () => {
    useThemePreference.getState().setScheme('light');

    expect(store.files.get(PREF_URI)).toBe(LIGHT_ON_DISK);
  });

  it('logs the scheme it could not persist when the file store refuses the write', () => {
    setThemePreferenceFileStore(storeThatRefusesWrites());

    useThemePreference.getState().setScheme('light');

    expect(warnSpy).toHaveBeenCalledWith(expect.stringContaining('light'), DISK_FULL);
  });
});
