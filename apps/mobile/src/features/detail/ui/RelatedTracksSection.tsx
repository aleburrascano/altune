/**
 * RelatedTracksSection — "Tracks you might like" rail on track detail.
 *
 * Renders nothing unless the track carries a SoundCloud source and the endpoint
 * returns a non-empty set (spec AC#4/AC#6/AC#7). Cards reuse the content-item
 * navigation (handoff push) used by the artist/album bodies, so a tap opens the
 * related track's own detail — chainable seed → related → related (AC#8).
 */

import type { ReactElement } from 'react';
import { Pressable, ScrollView, StyleSheet, View } from 'react-native';

import { useRouter } from 'expo-router';

import { Artwork } from '@shared/ui/primitives/Artwork';
import { Text } from '@shared/ui/primitives/Text';
import { radius, spacing } from '@shared/ui/theme/tokens';

import type { DiscoveryResult } from '@shared/api-client/discovery';

import { useRelatedTracks } from '../hooks/useRelatedTracks';
import { openDetail, type DetailRoute } from '../navigation';
import { sharedStyles } from './helpers';

const CARD_WIDTH = 132;

export function RelatedTracksSection({
  result,
  detailRoute,
}: {
  result: DiscoveryResult;
  detailRoute: DetailRoute;
}): ReactElement | null {
  const router = useRouter();
  const { relatedTracks } = useRelatedTracks({ sources: result.sources });

  // Hidden by construction for non-SoundCloud results (no fetch), on empty sets,
  // and on failure (the hook returns []). No empty-rail shell.
  if (relatedTracks.length === 0) {
    return null;
  }

  const onRelatedPress = (track: DiscoveryResult): void => {
    openDetail(router, detailRoute, { ...track, image_url: track.image_url ?? result.image_url });
  };

  return (
    <View testID="detail-related" style={styles.section}>
      <Text variant="label" tone="secondary" style={sharedStyles.sectionTitle}>
        Tracks you might like
      </Text>
      <ScrollView
        horizontal
        showsHorizontalScrollIndicator={false}
        contentContainerStyle={styles.rail}
      >
        {relatedTracks.map((track, index) => (
          <Pressable
            key={track.sources[0]?.external_id ?? index}
            testID={`detail-related-${index}`}
            onPress={() => onRelatedPress(track)}
            accessibilityRole="button"
            accessibilityLabel={`Open ${track.title}`}
            accessibilityHint="Opens the related track's detail"
            style={({ pressed }) => [styles.card, pressed ? { opacity: 0.6 } : null]}
          >
            <Artwork
              uri={track.image_url}
              size={CARD_WIDTH}
              radius={radius.md}
              accessibilityLabel={track.title}
            />
            <Text variant="body" numberOfLines={1} style={styles.cardTitle}>
              {track.title}
            </Text>
            {track.subtitle ? (
              <Text variant="label" tone="secondary" numberOfLines={1}>
                {track.subtitle}
              </Text>
            ) : null}
          </Pressable>
        ))}
      </ScrollView>
    </View>
  );
}

const styles = StyleSheet.create({
  section: { marginTop: spacing['2xl'] },
  rail: { gap: spacing.md, paddingVertical: spacing.sm },
  card: { width: CARD_WIDTH, gap: spacing.xs },
  cardTitle: { marginTop: spacing.xs },
});
