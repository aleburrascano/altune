import type { BackfillFeaturedResult } from '@shared/apiClient/tracks';

import type { SettingsPort } from '@features/settings/ports/settingsPort';

export interface MemorySettingsSeed {
  pages?: BackfillFeaturedResult[];
}

const EXHAUSTED_MESSAGE = 'no backfill page left to serve';

export function createMemorySettingsPort(seed: MemorySettingsSeed = {}): SettingsPort {
  const pages = [...(seed.pages ?? [])];
  let served = 0;

  return {
    backfillFeaturedArtists: async () => {
      await Promise.resolve();
      const page = pages[served];
      if (page === undefined) throw new Error(EXHAUSTED_MESSAGE);
      served += 1;
      return page;
    },
  };
}
