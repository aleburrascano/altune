import type { useRouter } from 'expo-router';
import { useState, type ComponentProps, type ReactElement } from 'react';
import { FlatList, StyleSheet, View, type FlatListProps, type ListRenderItem } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { LinearGradient } from 'expo-linear-gradient';
import { EllipsisVertical } from 'lucide-react-native';

import type { PlaylistId } from '@shared/api-client/ids';
import { usePlayback } from '@shared/playback/usePlayback';
import { useQueuePlayback } from '@shared/playback/useQueuePlayback';
import { Button, Screen, Text, spacing, useTheme } from '@shared/ui';
import { IconButton } from '@shared/ui/primitives/IconButton';
import { ContextMenu } from '@shared/ui/primitives/ContextMenu';
import type { PlaylistDetailResponse, TrackResponse } from '@shared/api-client/types';
import { useAddTracksToPlaylist } from '@shared/playlists';

import { goBackOrToLibrary } from '../goBackOrToLibrary';
import { usePlaylistDelete } from '../hooks/usePlaylistDelete';
import { usePlaylistOfflineAction } from '../hooks/usePlaylistOfflineAction';
import { usePlaylistPlayback } from '../hooks/usePlaylistPlayback';
import { usePlaylistRename } from '../hooks/usePlaylistRename';
import { useRetryAcquisition } from '../hooks/useRetryAcquisition';
import { usePlaylistTrackRemoval } from '../hooks/usePlaylistTrackRemoval';
import type { TrackSelectionController } from '../hooks/useTrackSelection';
import { AddTracksToPlaylistModal } from './AddTracksToPlaylistModal';
import { BackHeader } from './BackHeader';
import { listContent } from './listContentStyles';
import { PlaylistHero } from './PlaylistHero';
import { PlaylistTrackRow, type PlaylistTrackRowProps } from './PlaylistTrackRow';
import { TrackSelectionOverlay } from './TrackSelectionOverlay';
import { useLibraryNavigation } from '../hooks/useLibraryNavigation';

type Router = ReturnType<typeof useRouter>;
type HeroProps = ComponentProps<typeof PlaylistHero>;
type RenameHeroProps = Pick<
  HeroProps,
  'isEditing' | 'editName' | 'onEditNameChange' | 'onStartEditing' | 'onConfirmRename'
>;
type ActionHeroProps = Pick<HeroProps, 'onPlay' | 'onShuffle' | 'onAddTracks'>;
type RowStateProps = Pick<PlaylistTrackRowProps, 'track' | 'selection' | 'playback' | 'retry'>;
type RowHandlerProps = Pick<PlaylistTrackRowProps, 'onPlay' | 'onOpen' | 'onMore'>;
type ModalProps = ComponentProps<typeof AddTracksToPlaylistModal>;
type MenuItems = ComponentProps<typeof ContextMenu>['items'];

export type ContentProps = {
  playlistId: PlaylistId;
  playlist: PlaylistDetailResponse;
  refreshing: boolean;
  onRefresh: () => void;
  router: Router;
};

type DetailActions = {
  rename: ReturnType<typeof usePlaylistRename>;
  onAddTracks: () => void;
  addVisible: boolean;
  onCloseAdd: () => void;
};

type DetailProps = ContentProps & DetailActions;

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

function PlaylistGradient(): ReactElement {
  const { color } = useTheme();
  return (
    <LinearGradient
      colors={[`${color.accent}30`, `${color.accent}08`, 'transparent']}
      style={styles.gradient}
      pointerEvents="none"
    />
  );
}

function OptionsButton(props: { onPress: () => void }): ReactElement {
  return (
    <IconButton
      icon={EllipsisVertical}
      size={20}
      onPress={props.onPress}
      accessibilityLabel="Playlist options"
    />
  );
}

function PlaylistBackHeader(props: { router: Router; onOptions: () => void }): ReactElement {
  const onBack = (): void => goBackOrToLibrary(props.router);
  return (
    <BackHeader onBack={onBack}>
      <OptionsButton onPress={props.onOptions} />
    </BackHeader>
  );
}

function editItems(props: DetailProps): MenuItems {
  return [
    { label: 'Add Tracks', onPress: props.onAddTracks },
    { label: 'Rename Playlist', onPress: props.rename.startEditing },
  ];
}

function menuItems(
  props: DetailProps,
  onDelete: () => void,
  offlineAction: MenuItems[number],
): MenuItems {
  const danger: MenuItems[number] = { label: 'Delete Playlist', onPress: onDelete, tone: 'danger' };
  return [...editItems(props), offlineAction, danger];
}

function useMenuItems(props: DetailProps): MenuItems {
  const onDelete = usePlaylistDelete(props.playlistId, props.router);
  const offlineAction = usePlaylistOfflineAction(props.playlist.tracks);
  return menuItems(props, onDelete, offlineAction);
}

function useAnchorTop(): number {
  const insets = useSafeAreaInsets();
  return insets.top + spacing.xs + 44 + spacing.xs;
}

type MenuProps = DetailProps & { visible: boolean; onClose: () => void };

function PlaylistMenu(props: MenuProps): ReactElement {
  const anchorTop = useAnchorTop();
  const items = useMenuItems(props);
  const { visible, onClose } = props;
  return <ContextMenu visible={visible} onClose={onClose} anchorTop={anchorTop} items={items} />;
}

function PlaylistTopBar(props: DetailProps): ReactElement {
  const [visible, setVisible] = useState(false);
  return (
    <>
      <PlaylistGradient />
      <PlaylistBackHeader router={props.router} onOptions={() => setVisible(true)} />
      <PlaylistMenu {...props} visible={visible} onClose={() => setVisible(false)} />
    </>
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

function listProps(props: TrackListProps): Omit<FlatListProps<TrackResponse>, 'data'> {
  return {
    ...TRACK_LIST,
    onRefresh: props.onRefresh,
    refreshing: props.refreshing,
    ListHeaderComponent: <PlaylistHeroSection {...props} />,
    ListEmptyComponent: <EmptyTracks onAdd={props.onAddTracks} />,
    renderItem: renderTrack(props.ctx),
  };
}

function PlaylistTrackList(props: TrackListProps): ReactElement {
  return <FlatList {...listProps(props)} data={props.playlist.tracks} />;
}

function useRowServices(): Pick<RowContext, 'playback' | 'retry'> {
  return { playback: usePlayback(), retry: useRetryAcquisition() };
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

function PlaylistTracks(props: DetailProps): ReactElement {
  const ctx = useRowContext(props);
  return (
    <>
      <PlaylistTrackList {...props} ctx={ctx} />
      <SelectionOverlay controller={ctx.trackSelection} tracks={props.playlist.tracks} />
    </>
  );
}

function useAddTracks(props: DetailProps): Pick<ModalProps, 'adding' | 'onAdd'> {
  const addMut = useAddTracksToPlaylist();
  const { playlistId, onCloseAdd } = props;
  const onAdd: ModalProps['onAdd'] = (trackIds) => {
    addMut.mutate({ playlistId, trackIds }, { onSuccess: onCloseAdd });
  };
  return { adding: addMut.isPending, onAdd };
}

function addModalProps(props: DetailProps): Omit<ModalProps, 'adding' | 'onAdd'> {
  return {
    visible: props.addVisible,
    playlistName: props.playlist.name,
    existingTrackIds: props.playlist.tracks.map((t) => t.id),
    onClose: props.onCloseAdd,
  };
}

function PlaylistAddTracks(props: DetailProps): ReactElement {
  const add = useAddTracks(props);
  return <AddTracksToPlaylistModal {...addModalProps(props)} {...add} />;
}

function usePlaylistDetailActions(props: ContentProps): DetailActions {
  const [addVisible, setAddVisible] = useState(false);
  return {
    rename: usePlaylistRename(props.playlistId, props.playlist.name),
    onAddTracks: () => setAddVisible(true),
    onCloseAdd: () => setAddVisible(false),
    addVisible,
  };
}

export function PlaylistDetailContent(props: ContentProps): ReactElement {
  const actions = usePlaylistDetailActions(props);
  return (
    <Screen padded={false}>
      <PlaylistTopBar {...props} {...actions} />
      <PlaylistTracks {...props} {...actions} />
      <PlaylistAddTracks {...props} {...actions} />
    </Screen>
  );
}

const styles = StyleSheet.create({
  gradient: {
    position: 'absolute',
    top: 0,
    left: 0,
    right: 0,
    height: 350,
  },
  emptyTracks: {
    alignItems: 'center',
    gap: spacing.lg,
    paddingTop: spacing['2xl'],
  },
});
