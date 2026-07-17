/**
 * LastFmEnrichmentSection — the "About" block on artist detail (Editorial layout).
 *
 * Reads as an intentional editorial section, not a metadata dump and with no
 * provider attribution: a bio leads (with Read more), then a quiet
 * listeners · plays line, genre chips, and similar artists as name chips. The
 * Discogs facts (real name / aliases / members) were intentionally dropped — the
 * bio and "who they sound like" are the only parts worth surfacing.
 *
 * Renders nothing when there is no usable content.
 */

import { useState, type ReactElement } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';

import { Text } from '@shared/ui/primitives/Text';
import { useTheme } from '@shared/ui/theme/useTheme';
import { radius, spacing } from '@shared/ui/theme/tokens';

import type { LastFmEnrichmentResponse } from '@shared/api-client/enrichment';
import type { DiscoveryKind } from '@shared/api-client/discovery';

const MAX_TAGS = 4;
const MAX_SIMILAR = 6;
const BIO_COLLAPSED_LINES = 4;
const BIO_LONG_THRESHOLD = 220;

// compactCount renders a scrobble count as "5.2M" / "1.1B" / "950".
function compactCount(n: number): string {
  if (n >= 1_000_000_000) return `${(n / 1_000_000_000).toFixed(1)}B`;
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`;
  return String(n);
}

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
  const tags = enrichment.tags.slice(0, MAX_TAGS);
  const similar = kind === 'artist' ? enrichment.similar.slice(0, MAX_SIMILAR) : [];

  const popularity = [
    enrichment.listeners > 0 ? `${compactCount(enrichment.listeners)} listeners` : null,
    enrichment.playcount > 0 ? `${compactCount(enrichment.playcount)} plays` : null,
  ].filter((p): p is string => p !== null);

  const hasContent =
    popularity.length > 0 || tags.length > 0 || similar.length > 0 || bio !== '';
  if (!hasContent) {
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

      {popularity.length > 0 ? (
        <Text testID="detail-lastfm-popularity" variant="label" tone="tertiary">
          {popularity.join('  ·  ')}
        </Text>
      ) : null}

      {tags.length > 0 ? (
        <View style={styles.chips}>
          {tags.map((tag, index) => (
            <View
              key={tag}
              testID={`detail-lastfm-tag-${index}`}
              style={[styles.chip, { borderColor: theme.color.border }]}
            >
              <Text variant="caption" tone="secondary">
                {tag}
              </Text>
            </View>
          ))}
        </View>
      ) : null}

      {similar.length > 0 ? (
        <View testID="detail-lastfm-similar" style={styles.block}>
          <Text variant="label" tone="tertiary" style={styles.seclabel}>
            Similar artists
          </Text>
          <View style={styles.chips}>
            {similar.map((name, index) => (
              <View
                key={name}
                testID={`detail-lastfm-similar-${index}`}
                style={[styles.chip, { borderColor: theme.color.border }]}
              >
                <Text variant="caption" tone="secondary">
                  {name}
                </Text>
              </View>
            ))}
          </View>
        </View>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  section: { marginTop: spacing.sm, gap: spacing.md },
  block: { gap: spacing.sm },
  seclabel: { textTransform: 'uppercase', letterSpacing: 0.6 },
  chips: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: spacing.sm,
  },
  chip: {
    borderWidth: 1,
    borderRadius: radius.full,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.xs,
  },
});
