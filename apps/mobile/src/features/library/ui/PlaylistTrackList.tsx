import { type ComponentProps, type ReactElement } from 'react';
import { FlatList, StyleSheet, View, type FlatListProps, type ListRenderItem } from 'react-native';

import { usePlayback } from '@shared/playback/usePlayback';
import { useQueuePlayback } from '@shared/playback/useQueuePlayback';
import { Button, Text, spacing, useWideWebLayout } from '@shared/ui';
import type { TrackResponse } from '@shared/api-client/types';

import { usePlaylistPlayback } from '../hooks/usePlaylistPlayback';
import { useRetryAcquisition } from '../hooks/useRetryAcquisition';
import { usePlaylistTrackRemoval } from '../hooks/usePlaylistTrackRemoval';
import type { TrackSelectionController } from '../hooks/useTrackSelection';
import { useLibraryNavigation } from '../hooks/useLibraryNavigation';
import { listContent } from './listContentStyles';
import type { DetailActions, DetailProps } from './playlistDetailTypes';
import { PlaylistHero } from './PlaylistHero';
import { PlaylistTrackRow, type PlaylistTrackRowProps } from './PlaylistTrackRow';
import { TrackSelectionOverlay } from './TrackSelectionOverlay';

type HeroProps = ComponentProps<typeof PlaylistHero>;
type RenameHeroProps = Pick<
  HeroProps,
  'isEditing' | 'editName' | 'onEditNameChange' | 'onStartEditing' | 'onConfirmRename'
>;
type ActionHeroProps = Pick<HeroProps, 'onPlay' | 'onShuffle' | 'onAddTracks'>;
type RowStateProps = Pick<PlaylistTrackRowProps, 'track' | 'selection' | 'playback' | 'retry'>;
type RowHandlerProps = Pick<PlaylistTrackRowProps, 'onPlay' | 'onOpen' | 'onMore'>;

type RowContext = {
  trackSelection: TrackSelectionController;
  playlistPlayback: ReturnType<typeof usePlaylistPlayback>;
  navigateToTrack: ReturnType<typeof useLibraryNavigation>['navigateToTrack'];
  playback: ReturnType<typeof usePlayback>;
  retry: ReturnType<typeof useRetryAcquisition>;
};

type TrackListProps = DetailProps & { ctx: RowContext };

const TRACK_LIST = {
  keyExtractor: (track: TrackResponse): string => track.id,
  showsVerticalScrollIndicator: false,
  contentContainerStyle: listContent.padded,
};

function EmptyTracks({ onAdd }: { onAdd: () => void }): ReactElement {
  return (
    <View style={styles.emptyTracks}>
      <Text variant="label" tone="secondary">
        No tracks yet
      </Text>
      <Button label="Add Tracks" onPress={onAdd} />
    </View>
  );
}

function renameProps(rename: DetailActions['rename']): RenameHeroProps {
  return {
    isEditing: rename.isEditing,
    editName: rename.editName,
    onEditNameChange: rename.setEditName,
    onStartEditing: rename.startEditing,
    onConfirmRename: rename.confirmRename,
  };
}

function heroActions(props: TrackListProps): ActionHeroProps {
  const { play, shuffle } = props.ctx.playlistPlayback;
  return { onPlay: play, onShuffle: shuffle, onAddTracks: props.onAddTracks };
}

function PlaylistHeroSection(props: TrackListProps): ReactElement {
  const hero: HeroProps = {
    playlist: props.playlist,
    ...renameProps(props.rename),
    ...heroActions(props),
  };
  return <PlaylistHero {...hero} />;
}

function trackRowState(ctx: RowContext, track: TrackResponse): RowStateProps {
  const { selection } = ctx.trackSelection;
  return { track, selection, playback: ctx.playback, retry: ctx.retry };
}

function trackRowHandlers(ctx: RowContext, track: TrackResponse): RowHandlerProps {
  return {
    onPlay: () => ctx.playlistPlayback.playFrom(track.id),
    onOpen: () => ctx.navigateToTrack(track),
    onMore: (anchor) => ctx.trackSelection.onTrackMore(track, anchor),
  };
}

function TrackItem(props: { ctx: RowContext; item: TrackResponse }): ReactElement {
  const { ctx, item } = props;
  const row: PlaylistTrackRowProps = {
    ...trackRowState(ctx, item),
    ...trackRowHandlers(ctx, item),
  };
  return <PlaylistTrackRow {...row} />;
}

function renderTrack(ctx: RowContext): ListRenderItem<TrackResponse> {
  const renderItem: ListRenderItem<TrackResponse> = ({ item }) => (
    <TrackItem ctx={ctx} item={item} />
  );
  return renderItem;
}

function heroHeader(props: TrackListProps, includeHero: boolean) {
  return includeHero ? { ListHeaderComponent: <PlaylistHeroSection {...props} /> } : {};
}

function listProps(props: TrackListProps, includeHero: boolean): Omit<FlatListProps<TrackResponse>, 'data'> {
  return {
    ...TRACK_LIST,
    onRefresh: props.onRefresh,
    refreshing: props.refreshing,
    ...heroHeader(props, includeHero),
    ListEmptyComponent: <EmptyTracks onAdd={props.onAddTracks} />,
    renderItem: renderTrack(props.ctx),
  };
}

function PlaylistFlatList(props: TrackListProps & { includeHero: boolean }): ReactElement {
  return <FlatList {...listProps(props, props.includeHero)} data={props.playlist.tracks} />;
}

function PlaylistCompactLayout(props: TrackListProps): ReactElement {
  return <PlaylistFlatList {...props} includeHero />;
}

function PlaylistWideHero(props: TrackListProps): ReactElement {
  return (
    <View testID="playlist-wide-hero" style={styles.wideHero}>
      <PlaylistHeroSection {...props} />
    </View>
  );
}

function PlaylistWideTracks(props: TrackListProps): ReactElement {
  return (
    <View testID="playlist-wide-tracks" style={styles.wideTracks}>
      <PlaylistFlatList {...props} includeHero={false} />
    </View>
  );
}

function PlaylistWideLayout(props: TrackListProps): ReactElement {
  return (
    <View testID="playlist-wide-layout" style={styles.wideRow}>
      <PlaylistWideHero {...props} />
      <PlaylistWideTracks {...props} />
    </View>
  );
}

function useRowServices(): Pick<RowContext, 'playback' | 'retry'> {
  return { playback: usePlayback(), retry: useRetryAcquisition('playlist') };
}

function useRowContext(props: DetailProps): RowContext {
  const queue = useQueuePlayback();
  const { navigateToTrack } = useLibraryNavigation(props.router);
  const trackSelection = usePlaylistTrackRemoval({ ...props, queue, navigateToTrack });
  const playlistPlayback = usePlaylistPlayback(props.playlistId, props.playlist, queue);
  return { trackSelection, playlistPlayback, navigateToTrack, ...useRowServices() };
}

type OverlayProps = { controller: TrackSelectionController; tracks: TrackResponse[] };

function SelectionOverlay({ controller, tracks }: OverlayProps): ReactElement {
  const barVisible = controller.selection.active;
  return <TrackSelectionOverlay controller={controller} tracks={tracks} barVisible={barVisible} />;
}

export function PlaylistTrackList(props: DetailProps): ReactElement {
  const ctx = useRowContext(props);
  const isWide = useWideWebLayout();
  const full: TrackListProps = { ...props, ctx };
  return (
    <>
      {isWide ? <PlaylistWideLayout {...full} /> : <PlaylistCompactLayout {...full} />}
      <SelectionOverlay controller={ctx.trackSelection} tracks={props.playlist.tracks} />
    </>
  );
}

const styles = StyleSheet.create({
  emptyTracks: {
    alignItems: 'center',
    gap: spacing.lg,
    paddingTop: spacing['2xl'],
  },
  wideRow: { flexDirection: 'row', flex: 1 },
  wideHero: { width: 320 },
  wideTracks: { flex: 1 },
});
