import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { Artwork } from '@shared/ui/primitives/Artwork';
import { HorizontalRail } from '@shared/ui/primitives/HorizontalRail';
import { Tappable } from '@shared/ui/primitives/Tappable';
import { Text } from '@shared/ui/primitives/Text';
import { radius, spacing } from '@shared/ui/theme/tokens';

import type { DiscoveryResult } from '@shared/apiClient/discovery';

import { useRelatedTracks } from '@features/detail/hooks/useRelatedTracks';
import { type DetailRoute } from '@features/detail/domain/detailNavigation';
import { useOpenDetail } from '@features/detail/hooks/useDetailNavigation';
import { RELATED_CARD_WIDTH, sharedStyles } from '@features/detail/ui/detailStyles';

function relatedKey(track: DiscoveryResult, index: number): string {
  return track.sources[0]?.external_id ?? String(index);
}

function RelatedCard({
  track,
  index,
  onPress,
}: {
  track: DiscoveryResult;
  index: number;
  onPress: (track: DiscoveryResult) => void;
}): ReactElement {
  return (
    <Tappable
      action="detail.related_track.open"
      testID={`detail-related-${index}`}
      onPress={() => onPress(track)}
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
    </Tappable>
  );
}

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
      <HorizontalRail
        data={relatedTracks}
        keyExtractor={relatedKey}
        renderItem={({ item, index }) => (
          <RelatedCard track={item} index={index} onPress={onRelatedPress} />
        )}
        contentContainerStyle={styles.rail}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  section: { marginTop: spacing['2xl'] },
  rail: { gap: spacing.md, paddingVertical: spacing.sm },
  card: { width: RELATED_CARD_WIDTH, gap: spacing.xs },
  cardTitle: { marginTop: spacing.xs },
});
