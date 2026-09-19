import {
  createMemoryFileStore,
  type MemoryFileStore,
} from '@shared/files/__tests__/memoryFileStore';

import { setThemePreferenceFileStore, useThemePreference } from '../themePreference';

const PREF_URI = 'memory://document/preferences/theme.json';
const LIGHT_ON_DISK = '"light"';

let store: MemoryFileStore;

beforeEach(() => {
  store = createMemoryFileStore();
  setThemePreferenceFileStore(store);
});

afterEach(() => {
  setThemePreferenceFileStore();
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
});
