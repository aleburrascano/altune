import { useLocalSearchParams, useRouter } from 'expo-router';
import { useState, type ReactElement } from 'react';
import { FlatList, StyleSheet, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { LinearGradient } from 'expo-linear-gradient';
import { ChevronLeft, EllipsisVertical } from 'lucide-react-native';

import { NO_PLAYLIST_ID, parsePlaylistId } from '@shared/api-client/ids';
import { isCurrentlyPlaying } from '@shared/playback/isCurrentlyPlaying';
import { usePlayback } from '@shared/playback/usePlayback';
import { useQueuePlayback } from '@shared/playback/useQueuePlayback';
import { countLabel } from '@shared/lib/format';
import { Button, Screen, Skeleton, Text, spacing, useTheme } from '@shared/ui';
import { confirmDestructive } from '@shared/ui/confirmDestructive';
import { IconButton } from '@shared/ui/primitives/IconButton';
import { ContextMenu } from '@shared/ui/primitives/ContextMenu';
import type { TrackResponse } from '@shared/api-client/types';
import { useAddTracksToPlaylist, useRemoveTracksFromPlaylist } from '@shared/playlists';

import { goBackOrToLibrary } from '../goBackOrToLibrary';
import { usePlaylistDelete } from '../hooks/usePlaylistDelete';
import { usePlaylistDetail } from '../hooks/usePlaylistDetail';
import { usePlaylistOfflineAction } from '../hooks/usePlaylistOfflineAction';
import { usePlaylistPlayback } from '../hooks/usePlaylistPlayback';
import { usePlaylistRename } from '../hooks/usePlaylistRename';
import { useRetryAcquisition } from '../hooks/useRetryAcquisition';
import { useTrackSelection } from '../hooks/useTrackSelection';
import { AddTracksToPlaylistModal } from './AddTracksToPlaylistModal';
import { LibraryRow } from './LibraryRow';
import { listContent } from './listContentStyles';
import { PlaylistHero } from './PlaylistHero';
import { TrackSelectionOverlay } from './TrackSelectionOverlay';
import { useLibraryNavigation } from '../hooks/useLibraryNavigation';

const EMPTY_TRACKS: readonly TrackResponse[] = [];

export function PlaylistDetailScreen(): ReactElement {
  const router = useRouter();
  const params = useLocalSearchParams<{ id: string }>();
  // A deep-linked id is untrusted: anything that isn't a plausible id shape is treated as no id
  // at all (queries stay disabled, the screen redirects to the library).
  const parsedId = parsePlaylistId(params.id ?? '');
  const playlistId = parsedId.ok ? parsedId.id : NO_PLAYLIST_ID;

  const [menuVisible, setMenuVisible] = useState(false);
  const [addTracksVisible, setAddTracksVisible] = useState(false);

  const {
    data: playlistData,
    isLoading: playlistLoading,
    isRefetching: playlistRefetching,
    error: playlistError,
    refetch: refetchPlaylist,
  } = usePlaylistDetail(playlistId);

  const removeMut = useRemoveTracksFromPlaylist(playlistId);
  const addTracksMut = useAddTracksToPlaylist();

  const theme = useTheme();
  const insets = useSafeAreaInsets();
  const retryMut = useRetryAcquisition();
  const { navigateToTrack } = useLibraryNavigation(router);
  const playback = usePlayback();
  const queue = useQueuePlayback();

  const trackSelection = useTrackSelection({
    queue,
    onViewDetails: navigateToTrack,
    trackDanger: (track) => ({
      label: 'Remove from Playlist',
      onPress: () => removeMut.mutate([track.id]),
    }),
    selectionDanger: {
      label: 'Remove',
      onRemove: (ids, clear) =>
        confirmDestructive({
          title: 'Remove from Playlist',
          message: `Remove ${ids.length} ${countLabel(ids.length, 'track')} from ${playlistData?.name ?? ''}?`,
          confirmLabel: 'Remove',
          onConfirm: () => {
            removeMut.mutate(ids);
            clear();
          },
        }),
    },
  });
  const { selection } = trackSelection;

  const handleDelete = usePlaylistDelete(playlistId, router);
  const rename = usePlaylistRename(playlistId, playlistData?.name);
  const playlistPlayback = usePlaylistPlayback(playlistId, playlistData, queue);
  const offlineAction = usePlaylistOfflineAction(playlistData?.tracks ?? EMPTY_TRACKS);

  const goBack = () => goBackOrToLibrary(router);

  if (!playlistId) {
    router.replace('/library');
    return (
      <Screen>
        <View />
      </Screen>
    );
  }

  if (playlistLoading) {
    return (
      <Screen>
        <View style={styles.header}>
          <IconButton icon={ChevronLeft} size={24} onPress={goBack} accessibilityLabel="Back" />
        </View>
        <View style={styles.heroLoading}>
          <Skeleton width={160} height={160} radius={8} />
          <Skeleton width={200} height={20} />
          <Skeleton width={100} height={14} />
        </View>
      </Screen>
    );
  }

  if (playlistError || !playlistData) {
    return (
      <Screen>
        <View style={styles.header}>
          <IconButton icon={ChevronLeft} size={24} onPress={goBack} accessibilityLabel="Back" />
        </View>
        <View style={styles.center}>
          <Text variant="title">Playlist not found</Text>
          <Button label="Go back" onPress={() => router.replace('/library')} />
        </View>
      </Screen>
    );
  }

  const pl = playlistData;

  return (
    <Screen padded={false}>
      <LinearGradient
        colors={[`${theme.color.accent}30`, `${theme.color.accent}08`, 'transparent']}
        style={styles.gradient}
        pointerEvents="none"
      />
      <View style={styles.header}>
        <IconButton icon={ChevronLeft} size={24} onPress={goBack} accessibilityLabel="Back" />
        <IconButton
          icon={EllipsisVertical}
          size={20}
          onPress={() => setMenuVisible(true)}
          accessibilityLabel="Playlist options"
        />
      </View>

      <ContextMenu
        visible={menuVisible}
        onClose={() => setMenuVisible(false)}
        anchorTop={insets.top + spacing.xs + 44 + spacing.xs}
        items={[
          { label: 'Add Tracks', onPress: () => setAddTracksVisible(true) },
          { label: 'Rename Playlist', onPress: rename.startEditing },
          offlineAction,
          { label: 'Delete Playlist', onPress: handleDelete, tone: 'danger' },
        ]}
      />

      <FlatList
        data={pl.tracks}
        keyExtractor={(t) => t.id}
        showsVerticalScrollIndicator={false}
        onRefresh={() => {
          void refetchPlaylist();
        }}
        refreshing={playlistRefetching}
        contentContainerStyle={listContent.padded}
        ListHeaderComponent={
          <PlaylistHero
            playlist={pl}
            isEditing={rename.isEditing}
            editName={rename.editName}
            onEditNameChange={rename.setEditName}
            onStartEditing={rename.startEditing}
            onConfirmRename={rename.confirmRename}
            onPlay={playlistPlayback.play}
            onShuffle={playlistPlayback.shuffle}
            onAddTracks={() => setAddTracksVisible(true)}
          />
        }
        renderItem={({ item }) => (
          <View style={styles.trackRow}>
            <LibraryRow
              track={item}
              {...(item.acquisition_status === 'ready'
                ? { onPlay: () => playlistPlayback.playFrom(item.id) }
                : {})}
              onPress={() => navigateToTrack(item)}
              onMore={(anchor) => trackSelection.onTrackMore(item, anchor)}
              onLongPress={() => selection.begin(item.id)}
              {...(selection.active
                ? {
                    selectable: {
                      selected: selection.has(item.id),
                      onToggle: () => selection.toggle(item.id),
                    },
                  }
                : {})}
              {...(item.acquisition_status === 'failed'
                ? { onRetry: () => retryMut.mutate(item.id) }
                : {})}
              retrying={retryMut.isInFlight(item.id)}
              isPlaying={isCurrentlyPlaying(playback, { kind: 'library', trackId: item.id })}
            />
          </View>
        )}
        ListEmptyComponent={
          <View style={styles.emptyTracks}>
            <Text variant="label" tone="secondary">
              No tracks yet
            </Text>
            <Button label="Add Tracks" onPress={() => setAddTracksVisible(true)} />
          </View>
        }
      />

      <AddTracksToPlaylistModal
        visible={addTracksVisible}
        playlistName={pl.name}
        existingTrackIds={pl.tracks.map((t) => t.id)}
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
        tracks={pl.tracks}
        barVisible={selection.active}
      />
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
  header: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    paddingTop: spacing.xs,
    paddingHorizontal: spacing.lg,
  },
  heroLoading: {
    alignItems: 'center',
    gap: spacing.sm,
    paddingBottom: spacing.xl,
  },
  trackRow: { paddingHorizontal: spacing.lg },
  center: { flex: 1, alignItems: 'center', justifyContent: 'center', gap: spacing.lg },
  emptyTracks: {
    alignItems: 'center',
    gap: spacing.lg,
    paddingTop: spacing['2xl'],
  },
});
