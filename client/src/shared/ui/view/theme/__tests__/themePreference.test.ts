import {
  createMemoryFileStore,
  type MemoryFileStore,
} from '@testing/fakes/files/inMemoryFileStore';

import { createThemePreference } from '@shared/ui';

const PREF_URI = 'memory://document/preferences/theme.json';
const LIGHT_ON_DISK = '"light"';
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

type Warning = [string, ...unknown[]];

let store: MemoryFileStore;
let warnings: Warning[];

function themePreferenceOver(files: MemoryFileStore) {
  return createThemePreference({
    files,
    log: { warn: (message, ...details) => warnings.push([message, ...details]) },
  });
}

beforeEach(() => {
  store = createMemoryFileStore();
  warnings = [];
});

describe('theme preference', () => {
  it('starts dark when nothing was ever saved', () => {
    expect(themePreferenceOver(store).getState().scheme).toBe('dark');
  });

  it('starts on the scheme saved in the file store it is built over', () => {
    store.files.set(PREF_URI, LIGHT_ON_DISK);

    expect(themePreferenceOver(store).getState().scheme).toBe('light');
  });

  it('starts dark when the saved file is not a known scheme', () => {
    store.files.set(PREF_URI, '"sepia"');

    expect(themePreferenceOver(store).getState().scheme).toBe('dark');
  });

  it('saves the chosen scheme through the file store it is built over', () => {
    themePreferenceOver(store).getState().setScheme('light');

    expect(store.files.get(PREF_URI)).toBe(LIGHT_ON_DISK);
  });

  it('toggles between dark and light and saves each choice', () => {
    const preference = themePreferenceOver(store);

    preference.getState().toggle();
    expect(preference.getState().scheme).toBe('light');
    expect(store.files.get(PREF_URI)).toBe(LIGHT_ON_DISK);

    preference.getState().toggle();
    expect(preference.getState().scheme).toBe('dark');
  });

  it('logs the scheme it could not persist when the file store refuses the write', () => {
    themePreferenceOver(storeThatRefusesWrites()).getState().setScheme('light');

    expect(warnings).toEqual([[expect.stringContaining('light'), DISK_FULL]]);
  });

  it('still switches the scheme in memory when the write is refused', () => {
    const preference = themePreferenceOver(storeThatRefusesWrites());

    preference.getState().setScheme('light');

    expect(preference.getState().scheme).toBe('light');
  });
});
