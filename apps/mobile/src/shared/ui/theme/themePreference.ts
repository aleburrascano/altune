import { create } from 'zustand';

import { deviceFileStore, type FileStore, type StoredFile } from '@shared/files/fileStore';

import type { ColorScheme } from './theme';

const PREF_DIR = 'preferences';
const PREF_FILE = 'theme.json';

let fileStore: FileStore = deviceFileStore;

function prefFile(): StoredFile {
  const dir = fileStore.openDirectory(PREF_DIR);
  if (!dir.exists) dir.create();
  return dir.openFile(PREF_FILE);
}

function loadScheme(): ColorScheme {
  try {
    const file = prefFile();
    if (!file.exists) return 'dark';
    const parsed: unknown = JSON.parse(file.textSync());
    return parsed === 'light' ? 'light' : 'dark';
  } catch {
    return 'dark';
  }
}

function saveScheme(scheme: ColorScheme): void {
  try {
    prefFile().write(JSON.stringify(scheme));
  } catch (error) {
    console.warn(
      `[theme] could not persist the ${scheme} scheme; it will not survive a restart`,
      error,
    );
  }
}

export type ThemePreferenceState = {
  scheme: ColorScheme;
  setScheme: (scheme: ColorScheme) => void;
  toggle: () => void;
};

export const useThemePreference = create<ThemePreferenceState>((set, get) => ({
  scheme: loadScheme(),
  setScheme: (scheme) => {
    saveScheme(scheme);
    set({ scheme });
  },
  toggle: () => {
    const next: ColorScheme = get().scheme === 'dark' ? 'light' : 'dark';
    saveScheme(next);
    set({ scheme: next });
  },
}));

/**
 * Points the persisted preference at `store` (default: the device) and re-reads the scheme the way
 * a cold start does, since a scheme loaded from one filesystem says nothing about the next one's.
 */
export function setThemePreferenceFileStore(store: FileStore = deviceFileStore): void {
  fileStore = store;
  useThemePreference.setState({ scheme: loadScheme() });
}
