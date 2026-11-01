import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { Artwork } from '@shared/ui/primitives/Artwork';
import { Tappable } from '@shared/ui/primitives/Tappable';
import { Text } from '@shared/ui/primitives/Text';
import { radius, spacing } from '@shared/ui/theme';

import { realPhotoUrl } from '@features/detail/domain/artistPhoto';
import { hasManyVersions } from '@features/detail/domain/unreleasedTrack';
import type { DiscoveryResult } from '@shared/apiClient/discovery';
import type { UnreleasedTrack } from '@shared/apiClient/enrichment';

import type {
  UnreleasedCard as UnreleasedCardModel,
  UnreleasedCardState,
} from '@features/detail/hooks/useUnreleasedTracks';
import { RELATED_CARD_WIDTH, sharedStyles } from '@features/detail/ui/detailStyles';

type UnreleasedRowProps = { track: UnreleasedTrack };
type UnreleasedCardProps = { track: UnreleasedTrack; card: UnreleasedCardModel };
type VersionsButtonProps = { track: UnreleasedTrack; onOpen: () => void };
type PressState = { pressed: boolean };

type HintOf = (track: UnreleasedTrack) => string | null;

function noHint(): null {
  return null;
}

function fixedHint(text: string): HintOf {
  return function hint() {
    return text;
  };
}

function geniusHint(track: UnreleasedTrack): string | null {
  return track.has_media === false ? 'no audio found' : null;
}

function openHint(track: UnreleasedTrack): string {
  return `Open on ${track.open_host ?? 'link'}`;
}

const CARD_HINTS: Record<UnreleasedCardState, HintOf> = {
  genius: geniusHint,
  open: openHint,
  add: fixedHint('Tap to save'),
  saving: fixedHint('Downloading…'),
  ready: noHint,
  play: fixedHint('Tap to play'),
  failed: fixedHint("Couldn't get it · tap to retry"),
  rejected: fixedHint("Couldn't get it"),
  disabled: noHint,
};

function cardHint(track: UnreleasedTrack, state: UnreleasedCardState): string | null {
  return CARD_HINTS[state](track);
}

function VersionsButton({ track, onOpen }: VersionsButtonProps): ReactElement | null {
  if (!hasManyVersions(track)) return null;
  const count = track.versions.length;
  return (
    <Tappable
      action="detail.artist.unreleased.versions.open"
      testID={`detail-unreleased-versions-${track.key}`}
      accessibilityRole="button"
      accessibilityLabel={`Show ${count} versions of ${track.title}`}
      onPress={onOpen}
    >
      <Text variant="label" tone="tertiary">
        {`${count} versions`}
      </Text>
    </Tappable>
  );
}

function CardHint({ track, card }: UnreleasedCardProps): ReactElement | null {
  const hint = cardHint(track, card.state);
  if (hint === null) return null;
  return (
    <Text variant="label" tone="tertiary" testID={`detail-unreleased-hint-${track.key}`}>
      {hint}
    </Text>
  );
}

type UnreleasedCoverProps = { track: UnreleasedTrack; artist: DiscoveryResult };

function UnreleasedCover({ track, artist }: UnreleasedCoverProps): ReactElement {
  return (
    <Artwork
      uri={track.cover_url ?? realPhotoUrl(artist.image_url)}
      size={RELATED_CARD_WIDTH}
      radius={radius.md}
      accessibilityLabel={track.title}
    />
  );
}

function QualityLine({ track }: UnreleasedRowProps): ReactElement | null {
  if (track.quality === null) return null;
  return (
    <Text variant="label" tone="secondary" testID={`detail-unreleased-quality-${track.key}`}>
      {track.quality}
    </Text>
  );
}

function UnreleasedTitle({ track, card }: UnreleasedCardProps): ReactElement {
  return (
    <View style={styles.title}>
      <Text variant="body" numberOfLines={1}>
        {track.title}
      </Text>
      <QualityLine track={track} />
      <CardHint track={track} card={card} />
    </View>
  );
}

function pressedCardStyle({ pressed }: PressState) {
  return [styles.card, pressed ? sharedStyles.pressed : null];
}

type LabelOf = (track: UnreleasedTrack) => string;

function saveLabel(track: UnreleasedTrack): string {
  return `Save ${track.title}`;
}

function geniusLabel(track: UnreleasedTrack): string {
  return `Open ${track.title} on Genius`;
}

function openLabel(track: UnreleasedTrack): string {
  return `Open ${track.title} on ${track.open_host ?? 'link'}`;
}

function playLabel(track: UnreleasedTrack): string {
  return `Play ${track.title}`;
}

const CARD_LABELS: Record<UnreleasedCardState, LabelOf> = {
  genius: geniusLabel,
  open: openLabel,
  play: playLabel,
  add: saveLabel,
  saving: saveLabel,
  ready: saveLabel,
  failed: saveLabel,
  rejected: saveLabel,
  disabled: saveLabel,
};

function cardLabel(track: UnreleasedTrack, state: UnreleasedCardState): string {
  return CARD_LABELS[state](track);
}

const LINK_STATES: ReadonlySet<UnreleasedCardState> = new Set(['genius', 'open']);

function unreleasedRowProps({ track, card }: UnreleasedCardProps) {
  return {
    action: `detail.artist.unreleased.${card.state}`,
    testID: `detail-unreleased-${track.key}`,
    onPress: card.onPress,
    accessibilityRole: LINK_STATES.has(card.state) ? ('link' as const) : ('button' as const),
    accessibilityLabel: cardLabel(track, card.state),
    style: pressedCardStyle,
  };
}

type VersionsAccess = { onOpen: () => void; onLongPress: (() => void) | undefined };
type UnreleasedCardViewProps = UnreleasedCoverProps & {
  card: UnreleasedCardModel;
  versions: VersionsAccess;
};

export function UnreleasedCard({
  artist,
  track,
  card,
  versions,
}: UnreleasedCardViewProps): ReactElement {
  return (
    <View style={styles.column}>
      <Tappable {...unreleasedRowProps({ track, card })} onLongPress={versions.onLongPress}>
        <UnreleasedCover track={track} artist={artist} />
        <UnreleasedTitle track={track} card={card} />
      </Tappable>
      <VersionsButton track={track} onOpen={versions.onOpen} />
    </View>
  );
}

const styles = StyleSheet.create({
  column: { width: RELATED_CARD_WIDTH, gap: spacing.xs },
  card: { width: RELATED_CARD_WIDTH, gap: spacing.xs },
  title: { marginTop: spacing.xs },
});
