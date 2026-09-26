import { type ReactElement, type ReactNode } from 'react';
import { StyleSheet, View } from 'react-native';

import type { DiscoveryResult } from '@shared/api-client/discovery';
import { asyncView } from '@shared/lib/async-view';
import { AsyncSection } from '@shared/ui/AsyncSection';
import { Text } from '@shared/ui/primitives/Text';
import { spacing } from '@shared/ui/theme';

import type { AlbumDetailState } from '../hooks/useAlbumDetailState';

import { trackSubtitleWithFeaturing } from './formatters';
import { AlbumMoreTracks } from './AlbumMoreTracks';
import { AlbumTrackRow } from './AlbumTrackRow';
import { TrackRowsSkeleton } from './DetailSkeleton';
import { Section } from './Section';
import { SectionError } from './SectionError';

function TracksSection({ children }: { children: ReactNode }): ReactElement {
  return <Section label="Tracks">{children}</Section>;
}

function TracksSkeleton(): ReactElement {
  return (
    <TracksSection>
      <TrackRowsSkeleton testID="detail-tracklist-loading" />
    </TracksSection>
  );
}

function tracksErrorProps(album: AlbumDetailState) {
  return {
    testIDPrefix: 'detail-tracklist',
    message: "Couldn't load tracks.",
    onRetry: () => album.refetch(),
    failure: album.failure,
  };
}

function TracksError({ album }: { album: AlbumDetailState }): ReactElement {
  return (
    <TracksSection>
      <SectionError {...tracksErrorProps(album)} />
    </TracksSection>
  );
}

function EmptyPlaceholder(): ReactElement {
  return (
    <View testID="detail-tracklist-empty" style={styles.placeholder}>
      <Text variant="body" tone="tertiary">
        No tracks found.
      </Text>
    </View>
  );
}

function TracksEmpty(): ReactElement {
  return (
    <TracksSection>
      <EmptyPlaceholder />
    </TracksSection>
  );
}

function albumTrackRowMeta(album: AlbumDetailState, track: DiscoveryResult) {
  return {
    subtitle: trackSubtitleWithFeaturing(track),
    owned: album.ownedFor(track),
    savingInBatch: album.isSavingInBatch(track),
  };
}

function albumTrackRowProps(album: AlbumDetailState, track: DiscoveryResult, index: number) {
  return {
    track,
    index,
    ...albumTrackRowMeta(album, track),
    onPress: () => album.onTrackPress(track),
    onQuickSave: () => album.onQuickSave(track),
  };
}

function albumTrackRows(album: AlbumDetailState): ReactElement[] {
  return album.tracks.map((track, index) => (
    <AlbumTrackRow
      key={track.sources[0]?.external_id ?? `local-${index}`}
      {...albumTrackRowProps(album, track, index)}
    />
  ));
}

function TrackRows({ album }: { album: AlbumDetailState }): ReactElement {
  return <TracksSection>{albumTrackRows(album)}</TracksSection>;
}

function moreFromAlbumVisibility(album: AlbumDetailState) {
  return {
    tracks: album.moreTracks,
    baseIndex: album.tracks.length,
    expanded: album.moreExpanded,
    onToggle: () => album.setMoreExpanded((prev) => !prev),
  };
}

function moreFromAlbumSaveState(album: AlbumDetailState) {
  return { savingAll: album.savingAll, onSaveAll: album.onSaveAll };
}

function moreFromAlbumRowActions(album: AlbumDetailState) {
  return {
    ownedFor: album.ownedFor,
    isSavingInBatch: album.isSavingInBatch,
    onTrackPress: album.onTrackPress,
    onQuickSave: album.onQuickSave,
  };
}

function moreFromAlbumProps(album: AlbumDetailState) {
  return {
    ...moreFromAlbumVisibility(album),
    ...moreFromAlbumSaveState(album),
    rowActions: moreFromAlbumRowActions(album),
    failure: album.discoveryFailure,
    onRetry: album.discoveryRefetch,
  };
}

function MoreFromAlbum({ album }: { album: AlbumDetailState }): ReactElement | null {
  if (album.hasSources) return null;
  return <AlbumMoreTracks {...moreFromAlbumProps(album)} />;
}

function TrackListContent({ album }: { album: AlbumDetailState }): ReactElement {
  return (
    <View testID="detail-tracklist">
      <TrackRows album={album} />
      <MoreFromAlbum album={album} />
    </View>
  );
}

function trackListAsyncProps(album: AlbumDetailState) {
  return {
    view: tracksAsyncView(album),
    skeleton: TracksSkeleton,
    error: () => <TracksError album={album} />,
    empty: TracksEmpty,
  };
}

function tracksAsyncView(album: AlbumDetailState) {
  return asyncView({
    isLoading: album.isLoading,
    isError: album.isError,
    isEmpty: album.tracks.length === 0 && !album.moreExpanded && !album.discoveryError,
  });
}

export function AlbumTrackList({ album }: { album: AlbumDetailState }): ReactElement {
  return (
    <AsyncSection {...trackListAsyncProps(album)}>
      <TrackListContent album={album} />
    </AsyncSection>
  );
}

const styles = StyleSheet.create({
  placeholder: { alignItems: 'center', paddingVertical: spacing.lg },
});
