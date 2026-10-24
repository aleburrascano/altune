import type { ReactElement } from 'react';
import { Linking, StyleSheet, View } from 'react-native';

import { HorizontalRail } from '@shared/ui/primitives/HorizontalRail';
import { Artwork } from '@shared/ui/primitives/Artwork';
import { Tappable } from '@shared/ui/primitives/Tappable';
import { Text } from '@shared/ui/primitives/Text';
import { radius, spacing } from '@shared/ui/theme';

import type { DiscoveryResult } from '@shared/apiClient/discovery';
import type { UnreleasedTrack } from '@shared/apiClient/enrichment';

import { useUnreleasedTracks } from '@features/detail/hooks/useUnreleasedTracks';
import { DETAIL_GUTTER, RELATED_CARD_WIDTH, sharedStyles } from '@features/detail/ui/detailStyles';
import { Section } from '@features/detail/ui/Section';

type UnreleasedRowProps = { track: UnreleasedTrack };
type UnreleasedSectionProps = { tracks: UnreleasedTrack[] };
type ArtistUnreleasedProps = { artist: DiscoveryResult };
type PressState = { pressed: boolean };

function NoAudioHint({ track }: UnreleasedRowProps): ReactElement | null {
  if (track.has_media !== false) return null;
  return (
    <Text variant="label" tone="tertiary" testID={`detail-unreleased-no-audio-${track.genius_id}`}>
      no audio found
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

function UnreleasedTitle({ track }: UnreleasedRowProps): ReactElement {
  return (
    <View style={styles.title}>
      <Text variant="body" numberOfLines={1}>
        {track.title}
      </Text>
      <NoAudioHint track={track} />
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

function unreleasedRowProps(track: UnreleasedTrack) {
  return {
    action: 'detail.artist.unreleased',
    testID: `detail-unreleased-${track.genius_id}`,
    onPress: () => openGenius(track),
    accessibilityRole: 'link' as const,
    accessibilityLabel: `Open ${track.title} on Genius`,
    style: pressedCardStyle,
  };
}

function UnreleasedRow({ track }: UnreleasedRowProps): ReactElement {
  return (
    <Tappable {...unreleasedRowProps(track)}>
      <UnreleasedCover track={track} />
      <UnreleasedTitle track={track} />
    </Tappable>
  );
}

function unreleasedKey(track: UnreleasedTrack): string {
  return String(track.genius_id);
}

type UnreleasedItemInfo = { item: UnreleasedTrack };

function renderUnreleasedRow({ item }: UnreleasedItemInfo): ReactElement {
  return <UnreleasedRow track={item} />;
}

function UnreleasedSection({ tracks }: UnreleasedSectionProps): ReactElement {
  return (
    <View testID="detail-unreleased">
      <Section label="Unreleased">
        <HorizontalRail
          testID="detail-unreleased-rail"
          data={tracks}
          keyExtractor={unreleasedKey}
          renderItem={renderUnreleasedRow}
          style={styles.rail}
          contentContainerStyle={styles.railContent}
        />
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
  if (tracks.length > 0) return <UnreleasedSection tracks={tracks} />;
  return isLoading ? <UnreleasedLoading /> : null;
}

const styles = StyleSheet.create({
  rail: { marginHorizontal: -DETAIL_GUTTER },
  railContent: { paddingHorizontal: DETAIL_GUTTER, gap: spacing.md },
  card: { width: RELATED_CARD_WIDTH, gap: spacing.xs },
  title: { marginTop: spacing.xs },
});
