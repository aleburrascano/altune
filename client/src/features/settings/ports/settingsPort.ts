import type * as tracks from '@shared/api-client/tracks';

export interface SettingsPort {
  backfillFeaturedArtists: typeof tracks.backfillFeaturedArtists;
}
