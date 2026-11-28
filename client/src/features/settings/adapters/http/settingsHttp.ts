import { backfillFeaturedArtists } from '@shared/apiClient';

import { toBackfillFeaturedResult } from '@features/settings/adapters/http/mapping';
import type { SettingsPort } from '@features/settings/ports/ports';

export const httpSettingsPort: SettingsPort = {
  backfillFeaturedArtists: async (offset) =>
    toBackfillFeaturedResult(await backfillFeaturedArtists(offset)),
};
