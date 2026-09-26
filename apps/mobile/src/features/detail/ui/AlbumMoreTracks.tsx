import { type ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { Button } from '@shared/ui/primitives/Button';
import { spacing } from '@shared/ui/theme';

import type { DiscoveryResult } from '@shared/api-client/discovery';

import { type ContentFailure } from '../content-status';
import { type OwnedTrack } from '../hooks/useOwnedTrack';

import { trackSubtitleWithFeaturing } from './formatters';
import { AlbumTrackRow } from './AlbumTrackRow';
import { CollapsibleSectionHeader } from './CollapsibleSectionHeader';
import { SectionError } from './SectionError';

type AlbumMoreTracksProps = {
  tracks: DiscoveryResult[];
  baseIndex: number;
  expanded: boolean;
  onToggle: () => void;
  savingAll: boolean;
  onSaveAll: () => void;
  ownedFor: (track: DiscoveryResult) => OwnedTrack | null;
  isSavingInBatch: (track: DiscoveryResult) => boolean;
  onTrackPress: (track: DiscoveryResult) => void;
  onQuickSave: (track: DiscoveryResult) => void;
  failure: ContentFailure | null;
  onRetry: () => void;
};

type MoreTracksFailureProps = { failure: ContentFailure; onRetry: () => void };

function moreTracksErrorProps(props: MoreTracksFailureProps) {
  return {
    testIDPrefix: 'detail-more-from-album',
    message: "Couldn't load more tracks.",
    onRetry: props.onRetry,
    failure: props.failure,
  };
}

function MoreTracksFailure(props: MoreTracksFailureProps): ReactElement {
  return (
    <View style={styles.moreSection}>
      <CollapsibleSectionHeader label="More from this album" expanded={false} />
      <SectionError {...moreTracksErrorProps(props)} />
    </View>
  );
}

function trackRowMeta(props: AlbumMoreTracksProps, track: DiscoveryResult) {
  return {
    subtitle: trackSubtitleWithFeaturing(track),
    owned: props.ownedFor(track),
    savingInBatch: props.isSavingInBatch(track),
  };
}

function trackRowProps(props: AlbumMoreTracksProps, track: DiscoveryResult, index: number) {
  return {
    track,
    index: props.baseIndex + index,
    ...trackRowMeta(props, track),
    onPress: () => props.onTrackPress(track),
    onQuickSave: () => props.onQuickSave(track),
  };
}

function MoreTrackRows(props: AlbumMoreTracksProps): ReactElement[] {
  return props.tracks.map((track, index) => (
    <AlbumTrackRow
      key={track.sources[0]?.external_id ?? `more-${index}`}
      {...trackRowProps(props, track, index)}
    />
  ));
}

function moreSaveAllProps(props: AlbumMoreTracksProps) {
  return {
    testID: 'detail-save-all-more',
    label: props.savingAll ? 'Saving…' : 'Save all',
    variant: 'secondary' as const,
    onPress: props.onSaveAll,
    disabled: props.savingAll,
    style: styles.moreSaveAll,
  };
}

function MoreTracksExpanded(props: AlbumMoreTracksProps): ReactElement {
  return (
    <>
      <MoreTrackRows {...props} />
      <Button {...moreSaveAllProps(props)} />
    </>
  );
}

function moreTracksAccessibilityLabel(expanded: boolean): string {
  return expanded ? 'Collapse more tracks' : 'Show more from this album';
}

function moreTracksHeaderProps(props: AlbumMoreTracksProps) {
  return {
    label: 'More from this album',
    expanded: props.expanded,
    onToggle: props.onToggle,
    testID: 'detail-more-from-album',
    accessibilityLabel: moreTracksAccessibilityLabel(props.expanded),
  };
}

function MoreTracksReady(props: AlbumMoreTracksProps): ReactElement {
  return (
    <View style={styles.moreSection}>
      <CollapsibleSectionHeader {...moreTracksHeaderProps(props)} />
      {props.expanded ? <MoreTracksExpanded {...props} /> : null}
    </View>
  );
}

export function AlbumMoreTracks(props: AlbumMoreTracksProps): ReactElement | null {
  if (props.failure !== null) {
    return <MoreTracksFailure failure={props.failure} onRetry={props.onRetry} />;
  }
  if (props.tracks.length === 0) return null;
  return <MoreTracksReady {...props} />;
}

const styles = StyleSheet.create({
  moreSaveAll: { marginTop: spacing.lg },
  moreSection: { marginTop: spacing.xl },
});
