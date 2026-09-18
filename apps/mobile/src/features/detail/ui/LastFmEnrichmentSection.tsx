import { useState, type ReactElement } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';

import { ChevronRight } from 'lucide-react-native';

import { Artwork } from '@shared/ui/primitives/Artwork';
import { Text } from '@shared/ui/primitives/Text';
import { radius, spacing, useTheme } from '@shared/ui/theme';

import type { LastFmEnrichmentResponse } from '@shared/api-client/enrichment';

const MAX_SIMILAR = 6;
const BIO_COLLAPSED_LINES = 4;
const BIO_LONG_THRESHOLD = 220;

export function LastFmEnrichmentSection({
  enrichment,
  isError,
}: {
  enrichment: LastFmEnrichmentResponse | null;
  /** True only when the fetch failed — an artist with no Last.fm page is not an error. */
  isError: boolean;
}): ReactElement | null {
  if (enrichment === null) {
    // A failed fetch is retry-worthy where "no Last.fm page" is not, and both
    // arrive as a null enrichment. Keeping them apart in the tree is what lets a
    // later notice/retry treatment land here without re-plumbing the signal.
    return isError ? <View testID="detail-lastfm-unavailable" /> : null;
  }

  const bio = enrichment.bio;
  const similar = enrichment.similar.slice(0, MAX_SIMILAR);

  if (bio === '' && similar.length === 0) {
    return null;
  }

  return (
    <View testID="detail-lastfm" style={styles.section}>
      {bio !== '' ? <ArtistBio bio={bio} /> : null}
      {similar.length > 0 ? <SimilarArtists names={similar} /> : null}
    </View>
  );
}

// The bio's expand/collapse state is the only state in this file, so it lives
// with the one branch that can read it rather than above every early return.
function ArtistBio({ bio }: { bio: string }): ReactElement {
  const [expanded, setExpanded] = useState(false);
  const isLong = bio.length > BIO_LONG_THRESHOLD;

  return (
    <View style={styles.block}>
      <Text
        testID="detail-lastfm-bio"
        variant="body"
        tone="secondary"
        numberOfLines={expanded ? undefined : BIO_COLLAPSED_LINES}
      >
        {bio}
      </Text>
      {isLong ? (
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
  );
}

function SimilarArtists({ names }: { names: string[] }): ReactElement {
  const theme = useTheme();

  return (
    <View testID="detail-lastfm-similar">
      <Text variant="overline" tone="tertiary" style={styles.seclabel}>
        SIMILAR ARTISTS
      </Text>
      {names.map((name, index) => (
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
