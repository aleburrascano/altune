/**
 * The user's chosen colour scheme, persisted across launches.
 *
 * ADR-0008 shipped v1 dark-only and `lightTheme` was drafted but never visually
 * tuned. This exposes it behind an explicit user choice: the default stays dark,
 * so nobody sees light mode without asking for it.
 *
 * Persisted through `expo-file-system` (already a dependency) rather than a new
 * KV native module. Read synchronously at store creation so the first paint is
 * already the right scheme — a theme that flips one frame after launch is worse
 * than one that starts correct.
 */
import { Directory, File, Paths } from 'expo-file-system';
import { create } from 'zustand';

import type { ColorScheme } from './theme';

const PREF_DIR = 'preferences';
const PREF_FILE = 'theme.json';

function prefFile(): File {
  const dir = new Directory(Paths.document, PREF_DIR);
  if (!dir.exists) dir.create({ intermediates: true });
  return new File(dir, PREF_FILE);
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
  } catch {
    // Preference is cosmetic — a failed write just means it resets next launch.
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
