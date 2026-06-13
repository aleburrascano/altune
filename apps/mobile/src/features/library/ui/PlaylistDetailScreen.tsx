import { useLocalSearchParams, useRouter } from 'expo-router';
import { useCallback, useState, type ReactElement } from 'react';
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
import { setDetailHandoff } from '@shared/lib/detail-handoff';
import type { DiscoveryResult } from '@shared/api-client/discovery';
import type { TrackResponse } from '@shared/api-client/types';
import { Button, Screen, Skeleton, Text, spacing, useTheme } from '@shared/ui';

import { LibraryRow } from './LibraryRow';
import { PlaylistHero } from './PlaylistHero';

export function PlaylistDetailScreen(): ReactElement {
  const router = useRouter();
  const theme = useTheme();
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
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['playlist', playlistId] });
      void queryClient.invalidateQueries({ queryKey: ['playlists'] });
      setIsEditing(false);
    },
  });

  const deleteMut = useMutation({
    mutationFn: () => deletePlaylist(playlistId),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['playlists'] });
      if (router.canGoBack()) {
        router.back();
      } else {
        router.replace('/library');
      }
    },
  });

  const removeMut = useMutation({
    mutationFn: (trackId: string) => removeTrackFromPlaylist(playlistId, trackId),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['playlist', playlistId] });
      void queryClient.invalidateQueries({ queryKey: ['playlists'] });
    },
  });

  const navigateToTrack = useCallback(
    (track: TrackResponse): void => {
      const result: DiscoveryResult = {
        kind: 'track',
        title: track.title,
        subtitle: track.artist,
        image_url: track.artwork_url ?? null,
        confidence: 'high',
        sources: [],
        extras: {
          ...(track.album != null ? { album: track.album } : {}),
          ...(track.duration_seconds != null ? { duration_seconds: track.duration_seconds } : {}),
        },
      };
      setDetailHandoff(result);
      router.push('/library/detail');
    },
    [router],
  );

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
          <Pressable onPress={goBack} hitSlop={8}>
            <Text variant="body" style={{ color: theme.color.accent }}>← Back</Text>
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
          <Pressable onPress={goBack} hitSlop={8}>
            <Text variant="body" style={{ color: theme.color.accent }}>← Back</Text>
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
        >
          <Text variant="body" style={{ color: theme.color.accent }}>← Back</Text>
        </Pressable>
        <Pressable onPress={handleDelete} hitSlop={8} accessibilityRole="button" accessibilityLabel="Delete playlist">
          <Text variant="body" style={{ color: theme.color.danger }}>Delete</Text>
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
          <View style={styles.trackRowContainer}>
            <LibraryRow track={item} onPress={() => navigateToTrack(item)} />
            <Pressable
              testID={`playlist-remove-${item.id}`}
              onPress={() => removeMut.mutate(item.id)}
              hitSlop={8}
              style={styles.removeBtn}
              accessibilityRole="button"
              accessibilityLabel={`Remove ${item.title}`}
            >
              <Text variant="caption" style={{ color: theme.color.danger }}>✕</Text>
            </Pressable>
          </View>
        )}
        ListEmptyComponent={
          <View style={styles.emptyTracks}>
            <Text variant="label" tone="secondary">No tracks yet</Text>
            <Text variant="caption" tone="tertiary">Long-press any track to add it here</Text>
          </View>
        }
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
  heroLoading: {
    alignItems: 'center',
    gap: spacing.sm,
    paddingBottom: spacing.xl,
  },
  list: { paddingBottom: spacing['3xl'] },
  center: { flex: 1, alignItems: 'center', justifyContent: 'center', gap: spacing.lg },
  trackRowContainer: { flexDirection: 'row', alignItems: 'center' },
  removeBtn: {
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm,
  },
  emptyTracks: {
    alignItems: 'center',
    gap: spacing.xs,
    paddingTop: spacing['2xl'],
  },
});
