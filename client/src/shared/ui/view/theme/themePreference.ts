import { create } from 'zustand';

import type { FileStore, StoredFile } from '@shared/files';

import type { ColorScheme } from '@shared/ui/core/theme/theme';

const PREF_DIR = 'preferences';
const PREF_FILE = 'theme.json';

type ThemeLog = { warn: (message: string, ...details: unknown[]) => void };

type ThemePreferenceDeps = { files: FileStore; log: ThemeLog };

type ThemePreferenceState = {
  scheme: ColorScheme;
  setScheme: (scheme: ColorScheme) => void;
  toggle: () => void;
};

export function createThemePreference({ files, log }: ThemePreferenceDeps) {
  function prefFile(): StoredFile {
    const dir = files.openDirectory(PREF_DIR);
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
      log.warn(
        `[theme] could not persist the ${scheme} scheme; it will not survive a restart`,
        error,
      );
    }
  }

  return create<ThemePreferenceState>((set, get) => ({
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
}
