import type { SettingsPort } from '@features/settings';

type BackfillPage = Awaited<ReturnType<SettingsPort['backfillFeaturedArtists']>>;

export interface MemorySettingsSeed {
  pages?: BackfillPage[];
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
