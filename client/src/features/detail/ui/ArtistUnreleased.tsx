import type { ReactElement } from 'react';
import { Linking, View } from 'react-native';

import { Artwork } from '@shared/ui/primitives/Artwork';
import { Tappable } from '@shared/ui/primitives/Tappable';
import { Text } from '@shared/ui/primitives/Text';
import { radius } from '@shared/ui/theme';

import type { DiscoveryResult } from '@shared/apiClient/discovery';
import type { UnreleasedTrack } from '@shared/apiClient/enrichment';

import { useUnreleasedTracks } from '@features/detail/hooks/useUnreleasedTracks';
import { sharedStyles } from '@features/detail/ui/detailStyles';
import { Section } from '@features/detail/ui/Section';

const COVER_SIZE = 40;

type UnreleasedRowProps = { track: UnreleasedTrack };
type UnreleasedSectionProps = { tracks: UnreleasedTrack[] };
type ArtistUnreleasedProps = { artist: DiscoveryResult };
type PressState = { pressed: boolean };

function NoAudioHint({ track }: UnreleasedRowProps): ReactElement | null {
  if (track.has_media) return null;
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
      size={COVER_SIZE}
      radius={radius.sm}
      accessibilityLabel={track.title}
    />
  );
}

function UnreleasedTitle({ track }: UnreleasedRowProps): ReactElement {
  return (
    <View style={sharedStyles.trackInfo}>
      <Text variant="body" numberOfLines={1}>
        {track.title}
      </Text>
      <NoAudioHint track={track} />
    </View>
  );
}

function pressedRowStyle({ pressed }: PressState) {
  return [sharedStyles.trackRow, pressed ? sharedStyles.pressed : null];
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
    style: pressedRowStyle,
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

function renderUnreleasedRow(track: UnreleasedTrack): ReactElement {
  return <UnreleasedRow key={track.genius_id} track={track} />;
}

function UnreleasedSection({ tracks }: UnreleasedSectionProps): ReactElement {
  return (
    <View testID="detail-unreleased">
      <Section label="Unreleased">{tracks.map(renderUnreleasedRow)}</Section>
    </View>
  );
}

export function ArtistUnreleased({ artist }: ArtistUnreleasedProps): ReactElement | null {
  const tracks = useUnreleasedTracks(artist);
  return tracks.length === 0 ? null : <UnreleasedSection tracks={tracks} />;
}
