import { useState, type ReactElement } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';

import { ChevronRight } from 'lucide-react-native';

import { Artwork } from '@shared/ui/primitives/Artwork';
import { Text } from '@shared/ui/primitives/Text';
import { radius, spacing, useTheme } from '@shared/ui/theme';

import type { LastFmEnrichmentResponse } from '@shared/api-client/enrichment';
import type { DiscoveryKind } from '@shared/api-client/discovery';

const MAX_SIMILAR = 6;
const BIO_COLLAPSED_LINES = 4;
const BIO_LONG_THRESHOLD = 220;

export function LastFmEnrichmentSection({
  kind,
  enrichment,
}: {
  kind: DiscoveryKind;
  enrichment: LastFmEnrichmentResponse | null;
}): ReactElement | null {
  const theme = useTheme();
  const [expanded, setExpanded] = useState(false);

  if (enrichment === null) {
    return null;
  }

  const bio = enrichment.bio;
  const longBio = bio.length > BIO_LONG_THRESHOLD;
  const similar = kind === 'artist' ? enrichment.similar.slice(0, MAX_SIMILAR) : [];

  if (bio === '' && similar.length === 0) {
    return null;
  }

  return (
    <View testID="detail-lastfm" style={styles.section}>
      {bio !== '' ? (
        <View style={styles.block}>
          <Text
            testID="detail-lastfm-bio"
            variant="body"
            tone="secondary"
            numberOfLines={expanded ? undefined : BIO_COLLAPSED_LINES}
          >
            {bio}
          </Text>
          {longBio ? (
            <Pressable
              testID="detail-lastfm-bio-toggle"
              onPress={() => setExpanded((v) => !v)}
              accessibilityRole="button"
              accessibilityLabel={expanded ? 'Show less' : 'Read more'}
              hitSlop={8}
            >
              <Text variant="label" tone="accent">
                {expanded ? 'Read less' : 'Read more'}
              </Text>
            </Pressable>
          ) : null}
        </View>
      ) : null}

      {similar.length > 0 ? (
        <View testID="detail-lastfm-similar">
          <Text variant="overline" tone="tertiary" style={styles.seclabel}>
            SIMILAR ARTISTS
          </Text>
          {similar.map((name, index) => (
            <View
              key={name}
              testID={`detail-lastfm-similar-${index}`}
              style={[styles.simRow, { borderBottomColor: theme.color.border }]}
            >
              <Artwork uri={null} size={36} radius={radius.full} />
              <Text variant="body" style={styles.simName} numberOfLines={1}>
                {name}
              </Text>
              <ChevronRight size={16} color={theme.color.textTertiary} />
            </View>
          ))}
        </View>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  section: { gap: spacing.lg },
  block: { gap: spacing.sm },
  seclabel: { marginBottom: spacing.sm },
  simRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.md,
    minHeight: 52,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  simName: { flex: 1 },
});
