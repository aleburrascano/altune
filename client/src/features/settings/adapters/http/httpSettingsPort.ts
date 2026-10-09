import { backfillFeaturedArtists } from '@shared/api-client/tracks';

import type { SettingsPort } from '@features/settings/ports/settingsPort';

export const httpSettingsPort: SettingsPort = {
  backfillFeaturedArtists: (...args) => backfillFeaturedArtists(...args),
};
