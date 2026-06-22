/**
 * EnrichmentSection — MusicBrainz metadata block on the detail screen.
 *
 * Renders curated genre pills (top 4), release year, and community rating when
 * present. Renders nothing when there is no enrichment or no textual metadata
 * (the HD artwork upgrade is handled separately on the hero). Kind-agnostic —
 * shown once below the hero for track/album/artist (musicbrainz-enrichment AC#8).
 */

import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { Text } from '@shared/ui/primitives/Text';
import { useTheme } from '@shared/ui/theme/useTheme';
import { radius, spacing } from '@shared/ui/theme/tokens';

import type { EnrichmentResponse } from '@shared/api-client/discovery';

const MAX_GENRES = 4;

export function EnrichmentSection({
  enrichment,
}: {
  enrichment: EnrichmentResponse | null;
}): ReactElement | null {
  const theme = useTheme();

  if (enrichment === null) {
    return null;
  }

  const genres = enrichment.genres.slice(0, MAX_GENRES);
  const hasMeta = genres.length > 0 || enrichment.year > 0 || enrichment.rating > 0;
  if (!hasMeta) {
    return null;
  }

  const facts = [
    enrichment.year > 0 ? String(enrichment.year) : null,
    enrichment.rating > 0 ? `★ ${enrichment.rating.toFixed(1)}` : null,
  ].filter((f): f is string => f !== null);

  return (
    <View testID="detail-enrichment" style={styles.section}>
      {genres.length > 0 ? (
        <View style={styles.chips}>
          {genres.map((genre, index) => (
            <View
              key={genre}
              testID={`detail-genre-${index}`}
              style={[styles.chip, { borderColor: theme.color.border }]}
            >
              <Text variant="caption" tone="secondary">
                {genre}
              </Text>
            </View>
          ))}
        </View>
      ) : null}
      {facts.length > 0 ? (
        <Text variant="label" tone="tertiary" style={styles.facts}>
          {facts.join('  ·  ')}
        </Text>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  section: { marginTop: spacing.xl, alignItems: 'center', gap: spacing.sm },
  chips: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    justifyContent: 'center',
    gap: spacing.sm,
  },
  chip: {
    borderWidth: 1,
    borderRadius: radius.full,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.xs,
  },
  facts: {},
});
