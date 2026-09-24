import { useLocalSearchParams, useRouter } from 'expo-router';
import { useState, type ReactElement } from 'react';
import { FlatList, StyleSheet, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { LinearGradient } from 'expo-linear-gradient';
import { EllipsisVertical } from 'lucide-react-native';

import { NO_PLAYLIST_ID, parsePlaylistId, type PlaylistId } from '@shared/api-client/ids';
import { usePlayback } from '@shared/playback/usePlayback';
import { useQueuePlayback } from '@shared/playback/useQueuePlayback';
import { Button, Screen, Skeleton, Text, spacing, useTheme } from '@shared/ui';
import { IconButton } from '@shared/ui/primitives/IconButton';
import { ContextMenu } from '@shared/ui/primitives/ContextMenu';
import type { PlaylistDetailResponse } from '@shared/api-client/types';
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
import { AddTracksToPlaylistModal } from './AddTracksToPlaylistModal';
import { BackHeader } from './BackHeader';
import { listContent } from './listContentStyles';
import { PlaylistDetailFailure } from './PlaylistDetailFailure';
import { PlaylistHero } from './PlaylistHero';
import { PlaylistTrackRow } from './PlaylistTrackRow';
import { TrackSelectionOverlay } from './TrackSelectionOverlay';
import { useLibraryNavigation } from '../hooks/useLibraryNavigation';

type PlaylistDetailContentProps = {
  playlistId: PlaylistId;
  playlist: PlaylistDetailResponse;
  refreshing: boolean;
  onRefresh: () => void;
  router: ReturnType<typeof useRouter>;
};

function PlaylistDetailLoading({ onBack }: { onBack: () => void }): ReactElement {
  return (
    <Screen>
      <BackHeader onBack={onBack} />
      <View style={styles.heroLoading}>
        <Skeleton width={160} height={160} radius={8} />
        <Skeleton width={200} height={20} />
        <Skeleton width={100} height={14} />
      </View>
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

function PlaylistDetailContent({
  playlistId,
  playlist,
  refreshing,
  onRefresh,
  router,
}: PlaylistDetailContentProps): ReactElement {
  const [menuVisible, setMenuVisible] = useState(false);
  const [addTracksVisible, setAddTracksVisible] = useState(false);
  const addTracksMut = useAddTracksToPlaylist();

  const theme = useTheme();
  const insets = useSafeAreaInsets();
  const retryMut = useRetryAcquisition();
  const { navigateToTrack } = useLibraryNavigation(router);
  const playback = usePlayback();
  const queue = useQueuePlayback();

  const trackSelection = usePlaylistTrackRemoval(playlistId, playlist.name, queue, navigateToTrack);
  const { selection } = trackSelection;

  const handleDelete = usePlaylistDelete(playlistId, router);
  const rename = usePlaylistRename(playlistId, playlist.name);
  const playlistPlayback = usePlaylistPlayback(playlistId, playlist, queue);
  const offlineAction = usePlaylistOfflineAction(playlist.tracks);
  const openAddTracks = (): void => setAddTracksVisible(true);

  return (
    <Screen padded={false}>
      <LinearGradient
        colors={[`${theme.color.accent}30`, `${theme.color.accent}08`, 'transparent']}
        style={styles.gradient}
        pointerEvents="none"
      />
      <BackHeader onBack={() => goBackOrToLibrary(router)}>
        <IconButton
          icon={EllipsisVertical}
          size={20}
          onPress={() => setMenuVisible(true)}
          accessibilityLabel="Playlist options"
        />
      </BackHeader>

      <ContextMenu
        visible={menuVisible}
        onClose={() => setMenuVisible(false)}
        anchorTop={insets.top + spacing.xs + 44 + spacing.xs}
        items={[
          { label: 'Add Tracks', onPress: openAddTracks },
          { label: 'Rename Playlist', onPress: rename.startEditing },
          offlineAction,
          { label: 'Delete Playlist', onPress: handleDelete, tone: 'danger' },
        ]}
      />

      <FlatList
        data={playlist.tracks}
        keyExtractor={(t) => t.id}
        showsVerticalScrollIndicator={false}
        onRefresh={onRefresh}
        refreshing={refreshing}
        contentContainerStyle={listContent.padded}
        ListHeaderComponent={
          <PlaylistHero
            playlist={playlist}
            isEditing={rename.isEditing}
            editName={rename.editName}
            onEditNameChange={rename.setEditName}
            onStartEditing={rename.startEditing}
            onConfirmRename={rename.confirmRename}
            onPlay={playlistPlayback.play}
            onShuffle={playlistPlayback.shuffle}
            onAddTracks={openAddTracks}
          />
        }
        renderItem={({ item }) => (
          <PlaylistTrackRow
            track={item}
            selection={selection}
            playback={playback}
            retry={retryMut}
            onPlay={() => playlistPlayback.playFrom(item.id)}
            onOpen={() => navigateToTrack(item)}
            onMore={(anchor) => trackSelection.onTrackMore(item, anchor)}
          />
        )}
        ListEmptyComponent={<EmptyTracks onAdd={openAddTracks} />}
      />

      <AddTracksToPlaylistModal
        visible={addTracksVisible}
        playlistName={playlist.name}
        existingTrackIds={playlist.tracks.map((t) => t.id)}
        adding={addTracksMut.isPending}
        onAdd={(trackIds) =>
          addTracksMut.mutate(
            { playlistId, trackIds },
            { onSuccess: () => setAddTracksVisible(false) },
          )
        }
        onClose={() => setAddTracksVisible(false)}
      />

      <TrackSelectionOverlay
        controller={trackSelection}
        tracks={playlist.tracks}
        barVisible={selection.active}
      />
    </Screen>
  );
}

export function PlaylistDetailScreen(): ReactElement {
  const router = useRouter();
  const params = useLocalSearchParams<{ id: string }>();
  // A deep-linked id is untrusted: anything that isn't a plausible id shape is treated as no id
  // at all (queries stay disabled, the screen redirects to the library).
  const parsedId = parsePlaylistId(params.id ?? '');
  const playlistId = parsedId.ok ? parsedId.id : NO_PLAYLIST_ID;

  const {
    data: playlistData,
    isLoading: playlistLoading,
    isRefetching: playlistRefetching,
    error: playlistError,
    refetch: refetchPlaylist,
  } = usePlaylistDetail(playlistId);

  useLoggedPlaylistDetailFailure(playlistId, playlistError);

  const goBack = () => goBackOrToLibrary(router);
  const refetch = (): void => {
    void refetchPlaylist();
  };

  if (!playlistId) {
    router.replace('/library');
    return (
      <Screen>
        <View />
      </Screen>
    );
  }

  if (playlistLoading) return <PlaylistDetailLoading onBack={goBack} />;

  if (playlistError || !playlistData) {
    return (
      <Screen>
        <BackHeader onBack={goBack} />
        <PlaylistDetailFailure
          error={playlistError}
          onRetry={refetch}
          onGoToLibrary={() => router.replace('/library')}
        />
      </Screen>
    );
  }

  return (
    <PlaylistDetailContent
      playlistId={playlistId}
      playlist={playlistData}
      refreshing={playlistRefetching}
      onRefresh={refetch}
      router={router}
    />
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
