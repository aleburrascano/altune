/**
 * LastFmEnrichmentSection — Last.fm metadata block on the detail screen.
 *
 * Renders the listen-based popularity (listeners / plays), weighted tags,
 * similar artists (artist kind only), and a bio/blurb (track & album only — the
 * artist bio is owned by the Discogs section, so it is not duplicated here).
 * Renders nothing when there is no enrichment (docs/providers/lastfm.md cap 3).
 */

import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { Text } from '@shared/ui/primitives/Text';
import { useTheme } from '@shared/ui/theme/useTheme';
import { radius, spacing } from '@shared/ui/theme/tokens';

import type {
  DiscoveryKind,
  LastFmEnrichmentResponse,
} from '@shared/api-client/discovery';

const MAX_TAGS = 5;
const MAX_SIMILAR = 6;

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

  if (enrichment === null) {
    return null;
  }

  const tags = enrichment.tags.slice(0, MAX_TAGS);
  const similar = kind === 'artist' ? enrichment.similar.slice(0, MAX_SIMILAR) : [];
  // The artist bio is rendered by the Discogs section; here it would duplicate.
  const bio = kind !== 'artist' ? enrichment.bio : '';

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
      <Text variant="label" tone="tertiary" style={styles.heading}>
        Last.fm
      </Text>

      {popularity.length > 0 ? (
        <Text testID="detail-lastfm-popularity" variant="label" tone="tertiary" style={styles.line}>
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

      {bio !== '' ? (
        <Text testID="detail-lastfm-bio" variant="body" tone="secondary" style={styles.line}>
          {bio}
        </Text>
      ) : null}

      {similar.length > 0 ? (
        <Text testID="detail-lastfm-similar" variant="caption" tone="tertiary" style={styles.line}>
          Similar artists: {similar.join(', ')}
        </Text>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  section: { marginTop: spacing.xl, alignItems: 'center', gap: spacing.sm },
  heading: { textAlign: 'center', textTransform: 'uppercase', letterSpacing: 1 },
  line: { textAlign: 'center' },
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
});
