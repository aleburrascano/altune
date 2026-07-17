import { useLocalSearchParams, useRouter } from 'expo-router';
import { useState, type ReactElement } from 'react';
import { Alert, FlatList, StyleSheet, View } from 'react-native';
import { useQuery } from '@tanstack/react-query';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { LinearGradient } from 'expo-linear-gradient';
import { ChevronLeft, EllipsisVertical } from 'lucide-react-native';

import { getPlaylist } from '@shared/api-client/playlists';
import { isCurrentlyPlaying } from '@shared/playback/isCurrentlyPlaying';
import { buildPlayableQueue } from '@shared/playback/playFromList';
import { usePlayback } from '@shared/playback/usePlayback';
import { useQueuePlayback } from '@shared/playback/useQueuePlayback';
import { playlistKeys } from '@shared/lib/query-keys';
import { Button, Screen, Skeleton, Text, spacing, useTheme } from '@shared/ui';
import { IconButton } from '@shared/ui/primitives/IconButton';
import { ContextMenu } from '@shared/ui/primitives/ContextMenu';
import type { MenuAnchor } from '@shared/ui/primitives/menuPlacement';
import type { TrackResponse } from '@shared/api-client/types';

import {
  useDeletePlaylist,
  useRemoveTrackFromPlaylist,
  useRenamePlaylist,
} from '../hooks/usePlaylistMutations';
import { useRetryAcquisition } from '../hooks/useRetryAcquisition';
import { LibraryRow } from './LibraryRow';
import { PlaylistHero } from './PlaylistHero';
import { buildTrackMenuItems } from './trackMenu';
import { useLibraryNavigation } from './useLibraryNavigation';

export function PlaylistDetailScreen(): ReactElement {
  const router = useRouter();
  const params = useLocalSearchParams<{ id: string }>();
  const playlistId = params.id ?? '';

  const [isEditing, setIsEditing] = useState(false);
  const [editName, setEditName] = useState('');
  const [menuVisible, setMenuVisible] = useState(false);

  const { data: playlistData, isLoading: playlistLoading, error: playlistError } = useQuery({
    queryKey: playlistKeys.detail(playlistId),
    queryFn: () => getPlaylist(playlistId),
    enabled: playlistId.length > 0,
    staleTime: Infinity, // SSE-covered (rename/remove/reorder patch it); F15
  });

  // Cache policy (optimistic patch, rollback, invalidate, failure alerts)
  // lives in usePlaylistMutations; this screen keeps navigation + edit state.
  const renameMut = useRenamePlaylist(playlistId);
  const deleteMut = useDeletePlaylist(playlistId);
  const removeMut = useRemoveTrackFromPlaylist(playlistId);

  const theme = useTheme();
  const insets = useSafeAreaInsets();
  const retryMut = useRetryAcquisition();
  const retryingTrackId = retryMut.isPending ? retryMut.variables : undefined;
  const { navigateToTrack } = useLibraryNavigation(router);
  const playback = usePlayback();
  const queue = useQueuePlayback();
  const [trackAction, setTrackAction] = useState<{ track: TrackResponse; anchor: MenuAnchor } | null>(null);

  const trackMenuItems = (track: TrackResponse) =>
    buildTrackMenuItems(track, {
      queue,
      onViewDetails: () => navigateToTrack(track),
      danger: { label: 'Remove from Playlist', onPress: () => removeMut.mutate(track.id) },
    });

  const handleDelete = () => {
    Alert.alert('Delete Playlist', 'This cannot be undone.', [
      { text: 'Cancel', style: 'cancel' },
      {
        text: 'Delete',
        style: 'destructive',
        onPress: () =>
          deleteMut.mutate(undefined, {
            onSuccess: () => {
              if (router.canGoBack()) {
                router.back();
              } else {
                router.replace('/library');
              }
            },
          }),
      },
    ]);
  };

  const startEditing = () => {
    if (playlistData) {
      setEditName(playlistData.name);
      setIsEditing(true);
    }
  };

  const confirmRename = () => {
    const trimmed = editName.trim();
    if (trimmed.length > 0 && trimmed !== playlistData?.name) {
      renameMut.mutate(trimmed, { onSettled: () => setIsEditing(false) });
    } else {
      setIsEditing(false);
    }
  };

  const handlePlay = () => {
    if (!playlistData) return;
    const { playable, startIndex } = buildPlayableQueue(playlistData.tracks, playlistData.tracks[0]?.id ?? '');
    if (playable.length > 0) {
      queue.playFromList(playable, startIndex, { kind: 'playlist', playlistId, name: playlistData.name });
    }
  };

  const handleShuffle = () => {
    if (!playlistData) return;
    const { playable } = buildPlayableQueue(playlistData.tracks, '');
    if (playable.length === 0) return;
    const randomIdx = Math.floor(Math.random() * playable.length);
    queue.playFromList(playable, randomIdx, { kind: 'playlist', playlistId, name: playlistData.name });
    queue.toggleShuffle();
  };

  const goBack = () => router.canGoBack() ? router.back() : router.replace('/library');

  if (!playlistId) {
    router.replace('/library');
    return <Screen><View /></Screen>;
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
        <IconButton icon={EllipsisVertical} size={20} onPress={() => setMenuVisible(true)} accessibilityLabel="Playlist options" />
      </View>

      <ContextMenu
        visible={menuVisible}
        onClose={() => setMenuVisible(false)}
        anchorTop={insets.top + spacing.xs + 44 + spacing.xs}
        items={[
          { label: 'Rename Playlist', onPress: startEditing },
          { label: 'Delete Playlist', onPress: handleDelete, tone: 'danger' },
        ]}
      />

      <FlatList
        data={pl.tracks}
        keyExtractor={(t) => t.id}
        showsVerticalScrollIndicator={false}
        contentContainerStyle={styles.list}
        ListHeaderComponent={
          <PlaylistHero
            playlist={pl}
            isEditing={isEditing}
            editName={editName}
            onEditNameChange={setEditName}
            onStartEditing={startEditing}
            onConfirmRename={confirmRename}
            onPlay={handlePlay}
            onShuffle={handleShuffle}
          />
        }
        renderItem={({ item }) => (
          <View style={styles.trackRow}>
            <LibraryRow
              track={item}
              {...(item.acquisition_status === 'ready' ? { onPlay: () => {
                const { playable, startIndex } = buildPlayableQueue(pl.tracks, item.id);
                queue.playFromList(playable, startIndex, { kind: 'playlist', playlistId, name: pl.name });
              } } : {})}
              onPress={() => navigateToTrack(item)}
              onMore={(anchor) => setTrackAction({ track: item, anchor })}
              {...(item.acquisition_status === 'failed' ? { onRetry: () => retryMut.mutate(item.id) } : {})}
              retrying={retryingTrackId === item.id}
              isPlaying={isCurrentlyPlaying(playback, { kind: 'library', trackId: item.id })}
            />
          </View>
        )}
        ListEmptyComponent={
          <View style={styles.emptyTracks}>
            <Text variant="label" tone="secondary">No tracks yet</Text>
            <Text variant="caption" tone="tertiary">Use the menu on any track to add it here</Text>
          </View>
        }
      />
      <ContextMenu
        visible={trackAction != null}
        anchor={trackAction?.anchor}
        items={trackAction != null ? trackMenuItems(trackAction.track) : []}
        onClose={() => setTrackAction(null)}
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
  list: { paddingBottom: spacing['3xl'] },
  trackRow: { paddingHorizontal: spacing.lg },
  center: { flex: 1, alignItems: 'center', justifyContent: 'center', gap: spacing.lg },
  emptyTracks: {
    alignItems: 'center',
    gap: spacing.xs,
    paddingTop: spacing['2xl'],
  },
});
