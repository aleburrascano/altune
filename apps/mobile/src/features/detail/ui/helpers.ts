import { StyleSheet } from 'react-native';

import type { DiscoveryResult } from '@shared/api-client/discovery';
import { spacing } from '@shared/ui/theme/tokens';

import { albumExtras } from '../extras-accessors';

export function _albumYear(album: DiscoveryResult): string | null {
  const ae = albumExtras(album.extras);
  if (ae.releaseDate != null) return ae.releaseDate.slice(0, 4);
  return ae.year;
}

export { isCurrentlyPlaying } from '@shared/playback/isCurrentlyPlaying';

export const sharedStyles = StyleSheet.create({
  trackRow: {
    flexDirection: 'row',
    alignItems: 'center',
    paddingVertical: spacing.md,
    gap: spacing.md,
    minHeight: 48,
  },
  trackInfo: { flex: 1 },
  retryButton: { marginTop: spacing.sm },
  sectionTitle: { marginBottom: spacing.sm },
  albumsSection: { marginTop: spacing.xl },
});
