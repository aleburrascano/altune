import type { ReactElement } from 'react';
import { Linking, StyleSheet, View } from 'react-native';

import { HorizontalRail } from '@shared/ui/primitives/HorizontalRail';
import { Artwork } from '@shared/ui/primitives/Artwork';
import { Tappable } from '@shared/ui/primitives/Tappable';
import { Text } from '@shared/ui/primitives/Text';
import { radius, spacing } from '@shared/ui/theme';

import type { DiscoveryResult } from '@shared/apiClient/discovery';
import type { UnreleasedTrack } from '@shared/apiClient/enrichment';

import {
  useUnreleasedCard,
  type UnreleasedCard,
  type UnreleasedCardState,
  useUnreleasedTracks,
} from '@features/detail/hooks/useUnreleasedTracks';
import { DETAIL_GUTTER, RELATED_CARD_WIDTH, sharedStyles } from '@features/detail/ui/detailStyles';
import { Section } from '@features/detail/ui/Section';

type UnreleasedRowProps = { track: UnreleasedTrack };
type UnreleasedArtistRowProps = { artist: DiscoveryResult; track: UnreleasedTrack };
type UnreleasedCardProps = { track: UnreleasedTrack; card: UnreleasedCard };
type UnreleasedListProps = { artist: DiscoveryResult; tracks: UnreleasedTrack[] };
type ArtistUnreleasedProps = { artist: DiscoveryResult };
type PressState = { pressed: boolean };

const CARD_HINTS: Record<UnreleasedCardState, string | null> = {
  genius: null,
  add: 'Tap to save',
  saving: 'Downloading…',
  ready: null,
  play: 'Tap to play',
  failed: "Couldn't get it · tap to retry",
  rejected: "Couldn't get it",
  disabled: null,
};

function cardHint(track: UnreleasedTrack, state: UnreleasedCardState): string | null {
  if (state === 'genius' && track.has_media === false) return 'no audio found';
  return CARD_HINTS[state];
}

function CardHint({ track, card }: UnreleasedCardProps): ReactElement | null {
  const hint = cardHint(track, card.state);
  if (hint === null) return null;
  return (
    <Text variant="label" tone="tertiary" testID={`detail-unreleased-hint-${track.genius_id}`}>
      {hint}
    </Text>
  );
}

function UnreleasedCover({ track }: UnreleasedRowProps): ReactElement {
  return (
    <Artwork
      uri={track.cover_url}
      size={RELATED_CARD_WIDTH}
      radius={radius.md}
      accessibilityLabel={track.title}
    />
  );
}

function UnreleasedTitle({ track, card }: UnreleasedCardProps): ReactElement {
  return (
    <View style={styles.title}>
      <Text variant="body" numberOfLines={1}>
        {track.title}
      </Text>
      <CardHint track={track} card={card} />
    </View>
  );
}

function pressedCardStyle({ pressed }: PressState) {
  return [styles.card, pressed ? sharedStyles.pressed : null];
}

function openGenius(track: UnreleasedTrack): void {
  Linking.openURL(track.genius_url).catch((error: unknown) => {
    console.warn('[detail] could not open the Genius page', {
      geniusId: track.genius_id,
      error: error instanceof Error ? error.message : String(error),
    });
  });
}

function cardLabel(track: UnreleasedTrack, state: UnreleasedCardState): string {
  if (state === 'genius') return `Open ${track.title} on Genius`;
  if (state === 'play') return `Play ${track.title}`;
  return `Save ${track.title}`;
}

function unreleasedRowProps({ track, card }: UnreleasedCardProps) {
  return {
    action: `detail.artist.unreleased.${card.state}`,
    testID: `detail-unreleased-${track.genius_id}`,
    onPress: card.onPress,
    accessibilityRole: card.state === 'genius' ? ('link' as const) : ('button' as const),
    accessibilityLabel: cardLabel(track, card.state),
    style: pressedCardStyle,
  };
}

function UnreleasedRow({ artist, track }: UnreleasedArtistRowProps): ReactElement {
  const card = useUnreleasedCard({ track, artist, openGenius: () => openGenius(track) });
  return (
    <Tappable {...unreleasedRowProps({ track, card })}>
      <UnreleasedCover track={track} />
      <UnreleasedTitle track={track} card={card} />
    </Tappable>
  );
}

function unreleasedKey(track: UnreleasedTrack): string {
  return String(track.genius_id);
}

type UnreleasedItemInfo = { item: UnreleasedTrack };

function rowRenderer(artist: DiscoveryResult) {
  return function renderUnreleasedRow({ item }: UnreleasedItemInfo): ReactElement {
    return <UnreleasedRow artist={artist} track={item} />;
  };
}

const RAIL_LAYOUT = { testID: 'detail-unreleased-rail', keyExtractor: unreleasedKey };

function UnreleasedRail({ artist, tracks }: UnreleasedListProps): ReactElement {
  const renderItem = rowRenderer(artist);
  return <HorizontalRail {...RAIL_LAYOUT} {...railStyle()} data={tracks} renderItem={renderItem} />;
}

function railStyle() {
  return { style: styles.rail, contentContainerStyle: styles.railContent };
}

function UnreleasedSection(props: UnreleasedListProps): ReactElement {
  return (
    <View testID="detail-unreleased">
      <Section label="Unreleased">
        <UnreleasedRail {...props} />
      </Section>
    </View>
  );
}

function LoadingHint(): ReactElement {
  return (
    <Text variant="caption" tone="tertiary">
      Looking for unreleased tracks…
    </Text>
  );
}

function UnreleasedLoading(): ReactElement {
  return (
    <View testID="detail-unreleased-loading">
      <Section label="Unreleased">
        <LoadingHint />
      </Section>
    </View>
  );
}

export function ArtistUnreleased({ artist }: ArtistUnreleasedProps): ReactElement | null {
  const { tracks, isLoading } = useUnreleasedTracks(artist);
  if (tracks.length > 0) return <UnreleasedSection artist={artist} tracks={tracks} />;
  return isLoading ? <UnreleasedLoading /> : null;
}

const styles = StyleSheet.create({
  rail: { marginHorizontal: -DETAIL_GUTTER },
  railContent: { paddingHorizontal: DETAIL_GUTTER, gap: spacing.md },
  card: { width: RELATED_CARD_WIDTH, gap: spacing.xs },
  title: { marginTop: spacing.xs },
});
