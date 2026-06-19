import { useLocalSearchParams, useRouter } from 'expo-router';
import { useState, type ReactElement } from 'react';
import {
  Alert,
  FlatList,
  Pressable,
  StyleSheet,
  View,
} from 'react-native';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import {
  deletePlaylist,
  getPlaylist,
  removeTrackFromPlaylist,
  renamePlaylist,
} from '@shared/api-client/playlists';
import { isCurrentlyPlaying } from '@shared/playback/isCurrentlyPlaying';
import { toPlaybackTrack } from '@shared/playback/toPlaybackTrack';
import { usePlayback } from '@shared/playback/usePlayback';
import { useQueuePlayback } from '@shared/playback/useQueuePlayback';
import { Button, Screen, Skeleton, Text, spacing } from '@shared/ui';
import { ActionSheet } from '@shared/ui/primitives/ActionSheet';
import type { TrackResponse } from '@shared/api-client/types';

import { useRetryAcquisition } from '../hooks/useRetryAcquisition';
import { LibraryRow } from './LibraryRow';
import { PlaylistHero } from './PlaylistHero';
import { useLibraryNavigation } from './useLibraryNavigation';

export function PlaylistDetailScreen(): ReactElement {
  const router = useRouter();
  const queryClient = useQueryClient();
  const params = useLocalSearchParams<{ id: string }>();
  const playlistId = params.id ?? '';

  const [isEditing, setIsEditing] = useState(false);
  const [editName, setEditName] = useState('');

  const { data: playlistData, isLoading: playlistLoading, error: playlistError } = useQuery({
    queryKey: ['playlist', playlistId],
    queryFn: () => getPlaylist(playlistId),
    enabled: playlistId.length > 0,
  });

  const renameMut = useMutation({
    mutationFn: (name: string) => renamePlaylist(playlistId, name),
    onMutate: async (name) => {
      await queryClient.cancelQueries({ queryKey: ['playlist', playlistId] });
      const previous = queryClient.getQueryData(['playlist', playlistId]);
      if (previous) {
        queryClient.setQueryData(['playlist', playlistId], { ...previous, name });
      }
      return { previous };
    },
    onError: (_err, _name, context) => {
      if (context?.previous) {
        queryClient.setQueryData(['playlist', playlistId], context.previous);
      }
      Alert.alert('Rename failed', 'Could not rename the playlist. Please try again.');
    },
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: ['playlist', playlistId] });
      void queryClient.invalidateQueries({ queryKey: ['playlists'] });
      setIsEditing(false);
    },
  });

  const deleteMut = useMutation({
    mutationFn: () => deletePlaylist(playlistId),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['playlists'] });
      void queryClient.invalidateQueries({ queryKey: ['playlist', playlistId] });
      if (router.canGoBack()) {
        router.back();
      } else {
        router.replace('/library');
      }
    },
    onError: () => {
      Alert.alert('Delete failed', 'Could not delete the playlist. Please try again.');
    },
  });

  const removeMut = useMutation({
    mutationFn: (trackId: string) => removeTrackFromPlaylist(playlistId, trackId),
    onMutate: async (trackId) => {
      await queryClient.cancelQueries({ queryKey: ['playlist', playlistId] });
      const previous = queryClient.getQueryData<{ tracks: Array<{ id: string }> }>(['playlist', playlistId]);
      if (previous) {
        queryClient.setQueryData(['playlist', playlistId], {
          ...previous,
          tracks: previous.tracks.filter((t) => t.id !== trackId),
        });
      }
      return { previous };
    },
    onError: (_err, _trackId, context) => {
      if (context?.previous) {
        queryClient.setQueryData(['playlist', playlistId], context.previous);
      }
      Alert.alert('Remove failed', 'Could not remove the track. Please try again.');
    },
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: ['playlist', playlistId] });
      void queryClient.invalidateQueries({ queryKey: ['playlists'] });
    },
  });

  const retryMut = useRetryAcquisition();
  const retryingTrackId = retryMut.isPending ? retryMut.variables : undefined;
  const { navigateToTrack } = useLibraryNavigation(router);
  const playback = usePlayback();
  const queue = useQueuePlayback();
  const [actionTrack, setActionTrack] = useState<TrackResponse | null>(null);

  const handleDelete = () => {
    Alert.alert('Delete Playlist', 'This cannot be undone.', [
      { text: 'Cancel', style: 'cancel' },
      { text: 'Delete', style: 'destructive', onPress: () => deleteMut.mutate() },
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
      renameMut.mutate(trimmed);
    } else {
      setIsEditing(false);
    }
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
          <Pressable onPress={goBack} hitSlop={8} accessibilityRole="button" accessibilityLabel="Back" style={styles.headerBtn}>
            <Text variant="body" tone="accent">‹ Back</Text>
          </Pressable>
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
          <Pressable onPress={goBack} hitSlop={8} accessibilityRole="button" accessibilityLabel="Back" style={styles.headerBtn}>
            <Text variant="body" tone="accent">‹ Back</Text>
          </Pressable>
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
    <Screen>
      <View style={styles.header}>
        <Pressable
          onPress={goBack}
          hitSlop={8}
          accessibilityRole="button"
          accessibilityLabel="Back"
          style={styles.headerBtn}
        >
          <Text variant="body" tone="accent">‹ Back</Text>
        </Pressable>
        <Pressable onPress={handleDelete} hitSlop={8} accessibilityRole="button" accessibilityLabel="Delete playlist" style={styles.headerBtn}>
          <Text variant="body" tone="danger">Delete</Text>
        </Pressable>
      </View>

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
          />
        }
        renderItem={({ item }) => (
          <LibraryRow
            track={item}
            {...(item.acquisition_status === 'ready' ? { onPlay: () => {
              const playableTracks = pl.tracks.filter((t) => t.acquisition_status === 'ready').map(toPlaybackTrack);
              const startIdx = playableTracks.findIndex((t) => t.source.kind === 'library' && t.source.trackId === item.id);
              queue.playFromList(playableTracks, Math.max(0, startIdx), { kind: 'playlist', playlistId, name: pl.name });
            } } : {})}
            onPress={() => navigateToTrack(item)}
            onMore={() => setActionTrack(item)}
            {...(item.acquisition_status === 'failed' ? { onRetry: () => retryMut.mutate(item.id) } : {})}
            retrying={retryingTrackId === item.id}
            isPlaying={isCurrentlyPlaying(playback, { kind: 'library', trackId: item.id })}
          />
        )}
        ListEmptyComponent={
          <View style={styles.emptyTracks}>
            <Text variant="label" tone="secondary">No tracks yet</Text>
            <Text variant="caption" tone="tertiary">Use the menu on any track to add it here</Text>
          </View>
        }
      />
      <ActionSheet
        visible={actionTrack != null}
        title={actionTrack?.title}
        subtitle={actionTrack != null ? actionTrack.artist : undefined}
        options={actionTrack != null ? [
          { label: 'View Details', onPress: () => navigateToTrack(actionTrack) },
          { label: 'Remove from Playlist', tone: 'danger' as const, onPress: () => removeMut.mutate(actionTrack.id) },
        ] : []}
        onClose={() => setActionTrack(null)}
      />
    </Screen>
  );
}

const styles = StyleSheet.create({
  header: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    paddingTop: spacing.sm,
    paddingBottom: spacing.md,
  },
  headerBtn: { minHeight: 48, justifyContent: 'center' as const },
  heroLoading: {
    alignItems: 'center',
    gap: spacing.sm,
    paddingBottom: spacing.xl,
  },
  list: { paddingBottom: spacing['3xl'] },
  center: { flex: 1, alignItems: 'center', justifyContent: 'center', gap: spacing.lg },
  emptyTracks: {
    alignItems: 'center',
    gap: spacing.xs,
    paddingTop: spacing['2xl'],
  },
});
