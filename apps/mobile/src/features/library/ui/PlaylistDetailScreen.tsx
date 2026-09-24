import { useLocalSearchParams, useRouter } from 'expo-router';
import { useState, type ComponentProps, type ReactElement } from 'react';
import { FlatList, StyleSheet, View, type FlatListProps, type ListRenderItem } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { LinearGradient } from 'expo-linear-gradient';
import { EllipsisVertical } from 'lucide-react-native';

import { NO_PLAYLIST_ID, parsePlaylistId, type PlaylistId } from '@shared/api-client/ids';
import { usePlayback } from '@shared/playback/usePlayback';
import { useQueuePlayback } from '@shared/playback/useQueuePlayback';
import { Button, Screen, Skeleton, Text, spacing, useTheme } from '@shared/ui';
import { IconButton } from '@shared/ui/primitives/IconButton';
import { ContextMenu } from '@shared/ui/primitives/ContextMenu';
import type { PlaylistDetailResponse, TrackResponse } from '@shared/api-client/types';
import { useAddTracksToPlaylist } from '@shared/playlists';

import { goBackOrToLibrary } from '../goBackOrToLibrary';
import { useLoggedPlaylistDetailFailure } from '../hooks/useLoggedPlaylistDetailFailure';
import { usePlaylistDelete } from '../hooks/usePlaylistDelete';
import { usePlaylistDetail } from '../hooks/usePlaylistDetail';
import { usePlaylistOfflineAction } from '../hooks/usePlaylistOfflineAction';
import { usePlaylistPlayback } from '../hooks/usePlaylistPlayback';
import { usePlaylistRename } from '../hooks/usePlaylistRename';
import { useRetryAcquisition } from '../hooks/useRetryAcquisition';
import { usePlaylistTrackRemoval } from '../hooks/usePlaylistTrackRemoval';
import type { TrackSelectionController } from '../hooks/useTrackSelection';
import { AddTracksToPlaylistModal } from './AddTracksToPlaylistModal';
import { BackHeader } from './BackHeader';
import { listContent } from './listContentStyles';
import { PlaylistDetailFailure } from './PlaylistDetailFailure';
import { PlaylistHero } from './PlaylistHero';
import { PlaylistTrackRow, type PlaylistTrackRowProps } from './PlaylistTrackRow';
import { TrackSelectionOverlay } from './TrackSelectionOverlay';
import { useLibraryNavigation } from '../hooks/useLibraryNavigation';

type Router = ReturnType<typeof useRouter>;
type HeroProps = ComponentProps<typeof PlaylistHero>;
type ModalProps = ComponentProps<typeof AddTracksToPlaylistModal>;
type MenuItems = ComponentProps<typeof ContextMenu>['items'];

type ContentProps = {
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

type ScreenQuery = {
  playlist: PlaylistDetailResponse | undefined;
  isLoading: boolean;
  isRefetching: boolean;
  error: Error | null;
  refetch: () => void;
};

type StatesProps = { router: Router; playlistId: PlaylistId; state: ScreenQuery };

const TRACK_LIST = {
  keyExtractor: (track: TrackResponse): string => track.id,
  showsVerticalScrollIndicator: false,
  contentContainerStyle: listContent.padded,
};

function HeroSkeleton(): ReactElement {
  return (
    <View style={styles.heroLoading}>
      <Skeleton width={160} height={160} radius={8} />
      <Skeleton width={200} height={20} />
      <Skeleton width={100} height={14} />
    </View>
  );
}

function PlaylistDetailLoading({ onBack }: { onBack: () => void }): ReactElement {
  return (
    <Screen>
      <BackHeader onBack={onBack} />
      <HeroSkeleton />
    </Screen>
  );
}

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

function PlaylistBackHeader(props: { router: Router; onOptions: () => void }): ReactElement {
  return (
    <BackHeader onBack={() => goBackOrToLibrary(props.router)}>
      <IconButton
        icon={EllipsisVertical}
        size={20}
        onPress={props.onOptions}
        accessibilityLabel="Playlist options"
      />
    </BackHeader>
  );
}

function menuItems(
  props: DetailProps,
  onDelete: () => void,
  offlineAction: MenuItems[number],
): MenuItems {
  return [
    { label: 'Add Tracks', onPress: props.onAddTracks },
    { label: 'Rename Playlist', onPress: props.rename.startEditing },
    offlineAction,
    { label: 'Delete Playlist', onPress: onDelete, tone: 'danger' },
  ];
}

type MenuProps = DetailProps & { visible: boolean; onClose: () => void };

function PlaylistMenu(props: MenuProps): ReactElement {
  const insets = useSafeAreaInsets();
  const onDelete = usePlaylistDelete(props.playlistId, props.router);
  const offlineAction = usePlaylistOfflineAction(props.playlist.tracks);
  const anchorTop = insets.top + spacing.xs + 44 + spacing.xs;
  const items = menuItems(props, onDelete, offlineAction);
  return (
    <ContextMenu
      visible={props.visible}
      onClose={props.onClose}
      anchorTop={anchorTop}
      items={items}
    />
  );
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

function renameProps(rename: DetailActions['rename']): Partial<HeroProps> {
  return {
    isEditing: rename.isEditing,
    editName: rename.editName,
    onEditNameChange: rename.setEditName,
    onStartEditing: rename.startEditing,
    onConfirmRename: rename.confirmRename,
  };
}

function heroActions(props: TrackListProps): Partial<HeroProps> {
  const { play, shuffle } = props.ctx.playlistPlayback;
  return { onPlay: play, onShuffle: shuffle, onAddTracks: props.onAddTracks };
}

function PlaylistHeroSection(props: TrackListProps): ReactElement {
  const hero = { ...renameProps(props.rename), ...heroActions(props) };
  return <PlaylistHero playlist={props.playlist} {...(hero as Omit<HeroProps, 'playlist'>)} />;
}

function trackRowState(ctx: RowContext, track: TrackResponse): Partial<PlaylistTrackRowProps> {
  const { selection } = ctx.trackSelection;
  return { track, selection, playback: ctx.playback, retry: ctx.retry };
}

function trackRowHandlers(ctx: RowContext, track: TrackResponse): Partial<PlaylistTrackRowProps> {
  return {
    onPlay: () => ctx.playlistPlayback.playFrom(track.id),
    onOpen: () => ctx.navigateToTrack(track),
    onMore: (anchor) => ctx.trackSelection.onTrackMore(track, anchor),
  };
}

function TrackItem(props: { ctx: RowContext; item: TrackResponse }): ReactElement {
  const { ctx, item } = props;
  const row = { ...trackRowState(ctx, item), ...trackRowHandlers(ctx, item) };
  return <PlaylistTrackRow {...(row as PlaylistTrackRowProps)} />;
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

function PlaylistDetailContent(props: ContentProps): ReactElement {
  const actions = usePlaylistDetailActions(props);
  return (
    <Screen padded={false}>
      <PlaylistTopBar {...props} {...actions} />
      <PlaylistTracks {...props} {...actions} />
      <PlaylistAddTracks {...props} {...actions} />
    </Screen>
  );
}

function useRouteParamPlaylistId(): PlaylistId {
  const params = useLocalSearchParams<{ id: string }>();
  const parsedId = parsePlaylistId(params.id ?? '');
  return parsedId.ok ? parsedId.id : NO_PLAYLIST_ID;
}

function usePlaylistScreenQuery(playlistId: PlaylistId): ScreenQuery {
  const { data: playlist, isLoading, isRefetching, error, refetch } = usePlaylistDetail(playlistId);
  useLoggedPlaylistDetailFailure(playlistId, error);
  return { playlist, isLoading, isRefetching, error, refetch: () => void refetch() };
}

function LibraryRedirect({ router }: { router: Router }): ReactElement {
  router.replace('/library');
  return (
    <Screen>
      <View />
    </Screen>
  );
}

type FailedProps = { state: ScreenQuery; onBack: () => void; onLibrary: () => void };

function PlaylistDetailFailedView({ state, onBack, onLibrary }: FailedProps): ReactElement {
  return (
    <Screen>
      <BackHeader onBack={onBack} />
      <PlaylistDetailFailure
        error={state.error}
        onRetry={state.refetch}
        onGoToLibrary={onLibrary}
      />
    </Screen>
  );
}

function loadedProps(props: StatesProps, playlist: PlaylistDetailResponse): ContentProps {
  const { router, playlistId, state } = props;
  return { playlistId, playlist, router, refreshing: state.isRefetching, onRefresh: state.refetch };
}

function PlaylistDetailStates(props: StatesProps): ReactElement {
  const { playlist, error, isLoading } = props.state;
  const goBack = (): void => goBackOrToLibrary(props.router);
  const onLibrary = (): void => props.router.replace('/library');
  if (isLoading) return <PlaylistDetailLoading onBack={goBack} />;
  if (error || !playlist) {
    return <PlaylistDetailFailedView state={props.state} onBack={goBack} onLibrary={onLibrary} />;
  }
  return <PlaylistDetailContent {...loadedProps(props, playlist)} />;
}

export function PlaylistDetailScreen(): ReactElement {
  const router = useRouter();
  const playlistId = useRouteParamPlaylistId();
  const state = usePlaylistScreenQuery(playlistId);
  if (!playlistId) return <LibraryRedirect router={router} />;
  return <PlaylistDetailStates router={router} playlistId={playlistId} state={state} />;
}

const styles = StyleSheet.create({
  gradient: {
    position: 'absolute',
    top: 0,
    left: 0,
    right: 0,
    height: 350,
  },
  heroLoading: {
    alignItems: 'center',
    gap: spacing.sm,
    paddingBottom: spacing.xl,
  },
  emptyTracks: {
    alignItems: 'center',
    gap: spacing.lg,
    paddingTop: spacing['2xl'],
  },
});
