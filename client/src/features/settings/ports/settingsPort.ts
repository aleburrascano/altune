import type * as apiClient from '@shared/apiClient';

export interface SettingsPort {
  backfillFeaturedArtists: typeof apiClient.backfillFeaturedArtists;
}
