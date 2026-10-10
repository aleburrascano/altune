import { backfillFeaturedArtists } from '@shared/apiClient/tracks';

import type { SettingsPort } from '@features/settings/ports/settingsPort';

export const httpSettingsPort: SettingsPort = {
  backfillFeaturedArtists: (...args) => backfillFeaturedArtists(...args),
};
