import type * as tracks from '@shared/apiClient/tracks';

export interface SettingsPort {
  backfillFeaturedArtists: typeof tracks.backfillFeaturedArtists;
}
