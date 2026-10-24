import type { ComponentProps, ReactElement } from 'react';
import { StyleSheet, View, type StyleProp, type ViewStyle } from 'react-native';

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

type RelatedCardProps = {
  track: DiscoveryResult;
  index: number;
  onPress: (track: DiscoveryResult) => void;
};

type TrackProps = { track: DiscoveryResult };
type A11yProps = Pick<
  TappableProps,
  'accessibilityLabel' | 'accessibilityHint' | 'accessibilityRole'
>;
type PressState = { pressed: boolean };
type TappableProps = ComponentProps<typeof Tappable>;

type RelatedTracksSectionProps = {
  result: DiscoveryResult;
  detailRoute: DetailRoute;
};

function RelatedCardSubtitle({ track }: TrackProps): ReactElement | null {
  return track.subtitle ? (
    <Text variant="label" tone="secondary" numberOfLines={1}>
      {track.subtitle}
    </Text>
  ) : null;
}

function RelatedCardText({ track }: TrackProps): ReactElement {
  return (
    <>
      <Text variant="body" numberOfLines={1} style={styles.cardTitle}>
        {track.title}
      </Text>
      <RelatedCardSubtitle track={track} />
    </>
  );
}

function RelatedCardArt({ track }: TrackProps): ReactElement {
  return (
    <Artwork
      uri={track.image_url}
      size={RELATED_CARD_WIDTH}
      radius={radius.md}
      accessibilityLabel={track.title}
    />
  );
}

function relatedCardStyle({ pressed }: PressState): StyleProp<ViewStyle> {
  return [styles.card, pressed ? sharedStyles.pressed : null];
}

function relatedCardA11y(track: DiscoveryResult): A11yProps {
  return {
    accessibilityRole: 'button',
    accessibilityLabel: `Open ${track.title}`,
    accessibilityHint: "Opens the related track's detail",
  };
}

function relatedCardProps(card: RelatedCardProps): TappableProps {
  const { track, index, onPress } = card;
  return {
    ...relatedCardA11y(track),
    action: 'detail.related_track.open',
    testID: `detail-related-${index}`,
    onPress: () => onPress(track),
    style: relatedCardStyle,
  };
}

function RelatedCard(card: RelatedCardProps): ReactElement {
  return (
    <Tappable {...relatedCardProps(card)}>
      <RelatedCardArt track={card.track} />
      <RelatedCardText track={card.track} />
    </Tappable>
  );
}

export function RelatedTracksSection({
  result: parent,
  detailRoute,
}: RelatedTracksSectionProps): ReactElement | null {
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
