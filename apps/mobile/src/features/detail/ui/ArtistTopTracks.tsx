import { useState, type ReactElement } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';

import { Artwork } from '@shared/ui/primitives/Artwork';
import { Text } from '@shared/ui/primitives/Text';
import { radius, spacing } from '@shared/ui/theme';

import type { DiscoveryResult } from '@shared/api-client/discovery';

import { formatDuration } from '@shared/lib/format';

import { trackExtras } from '../extras-accessors';
import type { ArtistDetailState } from '../hooks/useArtistDetailState';
import type { OwnedTrack } from '../hooks/useOwnedTrack';

import { sharedStyles } from './styles';
import { AsyncListSection } from './AsyncListSection';
import { TrackRowsSkeleton } from './DetailSkeleton';
import { Section } from './Section';
import { TrackSaveControl } from './TrackSaveControl';

const TRACK_CAP = 5;

type TopTracksProps = { artist: ArtistDetailState; artistResult: DiscoveryResult };
type TopTrackRowsProps = TopTracksProps & { showAllTracks: boolean };

type ArtistTopTrackRowProps = {
  track: DiscoveryResult;
  index: number;
  owned: OwnedTrack | null;
  fallbackArtist: string;
  onPress: () => void;
  onQuickSave: () => void;
};

function visibleTopTracks(artist: ArtistDetailState, showAllTracks: boolean): DiscoveryResult[] {
  if (artist.hasSources || showAllTracks) return artist.topTracks;
  return artist.topTracks.slice(0, TRACK_CAP);
}

function showAllAction(artist: ArtistDetailState, showAllTracks: boolean, onShowAll: () => void) {
  if (artist.hasSources || showAllTracks || artist.topTracks.length <= TRACK_CAP) return {};
  const label = `Show all ${artist.topTracks.length}`;
  return { action: { label, onPress: onShowAll, testID: 'detail-show-all-tracks' } };
}

function topTracksSectionProps(artist: ArtistDetailState, showAllTracks: boolean, onShowAll: () => void) {
  return {
    label: artist.hasSources ? 'Popular tracks' : 'Your tracks',
    ...showAllAction(artist, showAllTracks, onShowAll),
  };
}

function TopTracksSkeleton(): ReactElement {
  return <TrackRowsSkeleton testID="detail-top-tracks-loading" count={5} />;
}

function topTracksErrorProps(artist: ArtistDetailState) {
  return {
    testIDPrefix: 'detail-top-tracks',
    message: "Couldn't load tracks.",
    onRetry: () => artist.refetchTracks(),
    failure: artist.tracksFailure,
  };
}

const TOP_TRACKS_EMPTY = { message: 'No tracks found.', variant: 'body' as const, tone: 'tertiary' as const };

function topTracksAsyncProps(artist: ArtistDetailState) {
  return {
    isLoading: artist.isLoadingTracks,
    isError: artist.isErrorTracks,
    isEmpty: artist.topTracks.length === 0,
    skeleton: TopTracksSkeleton,
    error: topTracksErrorProps(artist),
    empty: TOP_TRACKS_EMPTY,
  };
}

function topTrackRowProps(artist: ArtistDetailState, artistResult: DiscoveryResult, track: DiscoveryResult, index: number) {
  return {
    track,
    index,
    owned: artist.ownedFor(track),
    fallbackArtist: artistResult.title,
    onPress: () => artist.onTrackPress(track),
    onQuickSave: () => artist.onQuickSave(track),
  };
}

function TopTrackRows({ artist, artistResult, showAllTracks }: TopTrackRowsProps): ReactElement[] {
  return visibleTopTracks(artist, showAllTracks).map((track, index) => (
    <ArtistTopTrackRow
      key={track.sources[0]?.external_id ?? index}
      {...topTrackRowProps(artist, artistResult, track, index)}
    />
  ));
}

function TopTracksAsyncList({ artist, artistResult, showAllTracks }: TopTrackRowsProps): ReactElement {
  return (
    <AsyncListSection {...topTracksAsyncProps(artist)}>
      <TopTrackRows artist={artist} artistResult={artistResult} showAllTracks={showAllTracks} />
    </AsyncListSection>
  );
}

export function ArtistTopTracks({ artist, artistResult }: TopTracksProps): ReactElement {
  const [showAllTracks, setShowAllTracks] = useState(false);
  const onShowAll = () => setShowAllTracks(true);
  return (
    <Section {...topTracksSectionProps(artist, showAllTracks, onShowAll)}>
      <TopTracksAsyncList artist={artist} artistResult={artistResult} showAllTracks={showAllTracks} />
    </Section>
  );
}

function TrackRank({ index }: { index: number }): ReactElement {
  return (
    <Text variant="label" tone="tertiary" style={styles.rank}>
      {index + 1}
    </Text>
  );
}

function TrackTitleLabel({ title }: { title: string }): ReactElement {
  return (
    <View style={sharedStyles.trackInfo}>
      <Text variant="body" numberOfLines={1}>
        {title}
      </Text>
    </View>
  );
}

function TrackIdentity({ track }: { track: DiscoveryResult }): ReactElement {
  return (
    <>
      <Artwork uri={track.image_url} size={40} radius={radius.sm} accessibilityLabel={track.title} />
      <TrackTitleLabel title={track.title} />
    </>
  );
}

function TrackDurationLabel({ track }: { track: DiscoveryResult }): ReactElement | null {
  const seconds = trackExtras(track.extras).durationSeconds;
  if (seconds == null) return null;
  return (
    <Text variant="label" tone="tertiary" style={styles.trackDuration}>
      {formatDuration(seconds)}
    </Text>
  );
}

function trackRowPressableProps(props: ArtistTopTrackRowProps) {
  return {
    testID: `detail-top-track-${props.index}`,
    onPress: props.onPress,
    accessibilityRole: 'button' as const,
    accessibilityLabel: `Play ${props.track.title}`,
  };
}

function pressedTrackRowStyle({ pressed }: { pressed: boolean }) {
  return [sharedStyles.trackRow, pressed ? sharedStyles.pressed : null];
}

function trackSaveProps(props: ArtistTopTrackRowProps) {
  return {
    testID: `detail-top-track-save-${props.index}`,
    owned: props.owned,
    title: props.track.title,
    artist: props.track.subtitle ?? props.fallbackArtist,
    onPress: props.onQuickSave,
  };
}

function ArtistTopTrackRow(props: ArtistTopTrackRowProps): ReactElement {
  return (
    <Pressable {...trackRowPressableProps(props)} style={pressedTrackRowStyle}>
      <TrackRank index={props.index} />
      <TrackIdentity track={props.track} />
      <TrackDurationLabel track={props.track} />
      <TrackSaveControl {...trackSaveProps(props)} />
    </Pressable>
  );
}

const styles = StyleSheet.create({
  rank: { width: 14, textAlign: 'center' },
  trackDuration: { marginRight: spacing.xs, fontVariant: ['tabular-nums'] },
});
