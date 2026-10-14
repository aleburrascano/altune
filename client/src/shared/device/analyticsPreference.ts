import { create } from 'zustand';

import { defaultFileStore } from '@shared/files/defaultFileStore';
import type { FileStore, StoredFile } from '@shared/files/fileStore';

const PREF_DIR = 'preferences';
const PREF_FILE = 'analytics.json';

let fileStore: FileStore = defaultFileStore;

function prefFile(): StoredFile {
  const dir = fileStore.openDirectory(PREF_DIR);
  if (!dir.exists) dir.create();
  return dir.openFile(PREF_FILE);
}

function loadShareAnalytics(): boolean {
  try {
    const file = prefFile();
    if (!file.exists) return true;
    const parsed: unknown = JSON.parse(file.textSync());
    return parsed !== false;
  } catch {
    return true;
  }
}

function saveShareAnalytics(on: boolean): void {
  try {
    prefFile().write(JSON.stringify(on));
  } catch (error) {
    console.warn(
      `[analytics] could not persist the share-analytics choice (${on}); it will not survive a restart`,
      error,
    );
  }
}

type AnalyticsPreferenceState = {
  shareAnalytics: boolean;
  setShareAnalytics: (on: boolean) => void;
};

export const useAnalyticsPreference = create<AnalyticsPreferenceState>((set) => ({
  shareAnalytics: loadShareAnalytics(),
  setShareAnalytics: (on) => {
    saveShareAnalytics(on);
    set({ shareAnalytics: on });
  },
}));

export function shareAnalytics(): boolean {
  return useAnalyticsPreference.getState().shareAnalytics;
}

export function setAnalyticsPreferenceFileStore(store: FileStore = defaultFileStore): void {
  fileStore = store;
  useAnalyticsPreference.setState({ shareAnalytics: loadShareAnalytics() });
}
