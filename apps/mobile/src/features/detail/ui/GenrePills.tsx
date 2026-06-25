/**
 * GenrePills — the deduped genre row that sits under the detail header.
 *
 * Genres are the track/album/artist's identity, kept as a single pill row
 * (curated MusicBrainz genres) instead of the four separate provider slabs the
 * old screen carried. Renders nothing when there are no genres.
 */

import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { Text } from '@shared/ui/primitives/Text';
import { useTheme } from '@shared/ui/theme/useTheme';
import { radius, spacing } from '@shared/ui/theme/tokens';

const MAX_GENRES = 4;

export function GenrePills({ genres }: { genres: string[] }): ReactElement | null {
  const theme = useTheme();
  const shown = genres.slice(0, MAX_GENRES);
  if (shown.length === 0) {
    return null;
  }
  return (
    <View testID="detail-genres" style={styles.row}>
      {shown.map((genre) => (
        <View key={genre} style={[styles.pill, { borderColor: theme.color.border }]}>
          <Text variant="caption" tone="secondary">
            {genre}
          </Text>
        </View>
      ))}
    </View>
  );
}

const styles = StyleSheet.create({
  row: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    justifyContent: 'center',
    gap: spacing.sm,
    marginTop: spacing.lg,
  },
  pill: {
    borderWidth: 1,
    borderRadius: radius.full,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.xs,
  },
});
