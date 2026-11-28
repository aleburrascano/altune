import type { BackfillFeaturedResult } from '@features/settings/core/backfillResult';

export interface SettingsPort {
  backfillFeaturedArtists(offset?: number): Promise<BackfillFeaturedResult>;
}
