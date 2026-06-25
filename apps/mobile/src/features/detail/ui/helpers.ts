/**
 * Shared helpers and styles for detail screen sub-components.
 */

import { StyleSheet } from 'react-native';

import type { QueryClient } from '@tanstack/react-query';
import type { DiscoveryResult } from '@shared/api-client/discovery';
import { spacing } from '@shared/ui/theme/tokens';

import { albumExtras } from '../extras-accessors';
import { findTrackInLibraryCache } from '../helpers/find-track-in-library-cache';

export function _albumYear(album: DiscoveryResult): string | null {
  const ae = albumExtras(album.extras);
  if (ae.releaseDate != null) return ae.releaseDate.slice(0, 4);
  return ae.year;
}

export function _isTrackInLibraryCache(
  queryClient: QueryClient,
  title: string,
  artist: string | null,
): boolean {
  return findTrackInLibraryCache(queryClient, title, artist) !== null;
}

export { isCurrentlyPlaying } from '@shared/playback/isCurrentlyPlaying';

/** Styles shared across multiple detail sub-components. */
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
