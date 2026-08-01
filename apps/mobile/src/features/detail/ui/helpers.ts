import { StyleSheet } from 'react-native';

import type { DiscoveryResult } from '@shared/api-client/discovery';
import { spacing } from '@shared/ui/theme/tokens';

import { albumExtras } from '../extras-accessors';

export function _albumYear(album: DiscoveryResult): string | null {
  const ae = albumExtras(album.extras);
  if (ae.releaseDate != null) return ae.releaseDate.slice(0, 4);
  return ae.year;
}

export function compactCount(n: number): string {
  if (n >= 1_000_000_000) return `${(n / 1_000_000_000).toFixed(1)}B`;
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`;
  return String(n);
}

export function formatRuntime(totalSeconds: number): string | null {
  if (totalSeconds <= 0) return null;
  const totalMinutes = Math.floor(totalSeconds / 60);
  const hours = Math.floor(totalMinutes / 60);
  const minutes = totalMinutes % 60;
  return hours > 0 ? `${hours} hr ${minutes} min` : `${minutes} min`;
}

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
