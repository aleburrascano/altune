import { backfillFeaturedArtists } from '@shared/apiClient';

import type { SettingsPort } from '@features/settings/ports/settingsPort';

export const httpSettingsPort: SettingsPort = {
  backfillFeaturedArtists: (...args) => backfillFeaturedArtists(...args),
};
