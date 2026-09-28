import type { ReactElement } from 'react';
import { Pressable, ScrollView, StyleSheet, View } from 'react-native';

import { Artwork } from '@shared/ui/primitives/Artwork';
import { Text } from '@shared/ui/primitives/Text';
import { radius, spacing } from '@shared/ui/theme/tokens';

import type { DiscoveryResult } from '@shared/api-client/discovery';

import { useRelatedTracks } from '../hooks/useRelatedTracks';
import { useOpenDetail, type DetailRoute } from '../navigation';
import { RELATED_CARD_WIDTH } from './layout';
import { sharedStyles } from './styles';

export function RelatedTracksSection({
  result: parent,
  detailRoute,
}: {
  result: DiscoveryResult;
  detailRoute: DetailRoute;
}): ReactElement | null {
  const openDetail = useOpenDetail(detailRoute);
  const { relatedTracks } = useRelatedTracks({ sources: parent.sources });

  if (relatedTracks.length === 0) {
    return null;
  }

  const onRelatedPress = (track: DiscoveryResult): void => {
    openDetail({ ...track, image_url: track.image_url ?? parent.image_url });
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
            style={({ pressed }) => [styles.card, pressed ? sharedStyles.pressed : null]}
          >
            <Artwork
              uri={track.image_url}
              size={RELATED_CARD_WIDTH}
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
  card: { width: RELATED_CARD_WIDTH, gap: spacing.xs },
  cardTitle: { marginTop: spacing.xs },
});
