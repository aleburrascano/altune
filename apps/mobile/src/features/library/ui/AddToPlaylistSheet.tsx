import { useCallback, useState, type ReactElement } from 'react';
import { Alert, FlatList, type ListRenderItemInfo, Modal, Pressable, StyleSheet, View } from 'react-native';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { addTrackToPlaylist, createPlaylist, getPlaylists } from '@shared/api-client/playlists';
import type { PlaylistResponse } from '@shared/api-client/types';
import { Text, spacing, useTheme } from '@shared/ui';

import { CreatePlaylistModal } from './CreatePlaylistModal';

type AddToPlaylistSheetProps = {
  visible: boolean;
  trackId: string;
  trackTitle: string;
  onClose: () => void;
};

export function AddToPlaylistSheet({
  visible,
  trackId,
  trackTitle,
  onClose,
}: AddToPlaylistSheetProps): ReactElement {
  const theme = useTheme();
  const queryClient = useQueryClient();
  const [createVisible, setCreateVisible] = useState(false);
  const [addedTo, setAddedTo] = useState<string | null>(null);

  const { data: playlistsData, isLoading: playlistsLoading } = useQuery({
    queryKey: ['playlists'],
    queryFn: getPlaylists,
    enabled: visible,
  });

  const addMut = useMutation({
    mutationFn: (playlistId: string) => addTrackToPlaylist(playlistId, { track_id: trackId }),
    onMutate: async (playlistId) => {
      await queryClient.cancelQueries({ queryKey: ['playlists'] });
      const previous = queryClient.getQueryData<{ items: PlaylistResponse[] }>(['playlists']);
      if (previous) {
        queryClient.setQueryData<{ items: PlaylistResponse[] }>(['playlists'], {
          ...previous,
          items: previous.items.map((p) =>
            p.id === playlistId ? { ...p, track_count: p.track_count + 1 } : p,
          ),
        });
      }
      return { previous };
    },
    onSuccess: (_data, playlistId) => {
      setAddedTo(playlistId);
    },
    onError: (_err, _playlistId, context) => {
      if (context?.previous) {
        queryClient.setQueryData(['playlists'], context.previous);
      }
      Alert.alert('Add failed', 'Could not add the track to the playlist. Please try again.');
    },
    onSettled: async (_data, _error, playlistId) => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['playlists'] }),
        queryClient.invalidateQueries({ queryKey: ['playlist', playlistId] }),
      ]);
      setAddedTo(null);
      onClose();
    },
  });

  const createMut = useMutation({
    mutationFn: async (name: string) => {
      const pl = await createPlaylist({ name });
      let addFailed = false;
      try {
        await addTrackToPlaylist(pl.id, { track_id: trackId });
      } catch {
        addFailed = true;
      }
      return { pl, addFailed };
    },
    onSuccess: ({ addFailed }) => {
      if (addFailed) {
        Alert.alert('Note', 'Playlist created, but the track could not be added. Try adding it manually.');
      }
    },
    onError: () => {
      Alert.alert('Error', 'Could not create the playlist. Please try again.');
    },
    onSettled: async () => {
      await queryClient.invalidateQueries({ queryKey: ['playlists'] });
      setCreateVisible(false);
      onClose();
    },
  });

  const playlists = playlistsData?.items ?? [];

  const renderPlaylistItem = useCallback(
    ({ item }: ListRenderItemInfo<PlaylistResponse>) => (
      <Pressable
        testID={`add-to-playlist-${item.id}`}
        onPress={() => addMut.mutate(item.id)}
        disabled={addMut.isPending}
        style={({ pressed }) => [
          styles.playlistRow,
          { borderBottomColor: theme.color.border },
          pressed ? styles.pressed : null,
        ]}
      >
        <View style={[styles.playlistIcon, { backgroundColor: theme.color.surface2 }]}>
          <Text variant="caption" tone="tertiary">♫</Text>
        </View>
        <View style={styles.playlistInfo}>
          <Text variant="body" numberOfLines={1}>{item.name}</Text>
          <Text variant="caption" tone="secondary">
            {item.track_count} {item.track_count === 1 ? 'track' : 'tracks'}
          </Text>
        </View>
        {addedTo === item.id ? (
          <Text variant="caption" style={{ color: theme.color.success }}>Added ✓</Text>
        ) : null}
      </Pressable>
    ),
    [addMut, addedTo, theme.color.border, theme.color.surface2, theme.color.success],
  );

  const handleClose = () => {
    setAddedTo(null);
    onClose();
  };

  return (
    <>
      <Modal
        testID="add-to-playlist-sheet"
        visible={visible && !createVisible}
        transparent
        animationType="slide"
        onRequestClose={handleClose}
      >
        <Pressable style={styles.backdrop} onPress={handleClose}>
          <View />
        </Pressable>
        <View style={[styles.sheet, { backgroundColor: theme.color.surface1 }]}>
          <View style={[styles.handle, { backgroundColor: theme.color.border }]} />
          <Text variant="title" style={styles.sheetTitle}>
            Add to Playlist
          </Text>
          <Text variant="caption" tone="secondary" numberOfLines={1} style={styles.trackLabel}>
            {trackTitle}
          </Text>

          <Pressable
            testID="add-to-playlist-create-new"
            onPress={() => setCreateVisible(true)}
            style={({ pressed }) => [
              styles.playlistRow,
              { borderBottomColor: theme.color.border },
              pressed ? styles.pressed : null,
            ]}
          >
            <View style={[styles.createIcon, { backgroundColor: theme.color.accent }]}>
              <Text variant="bodyStrong" tone="onAccent">+</Text>
            </View>
            <Text variant="bodyStrong">Create New Playlist</Text>
          </Pressable>

          <FlatList
            data={playlists}
            keyExtractor={(item) => item.id}
            style={styles.list}
            renderItem={renderPlaylistItem}
            ListEmptyComponent={
              playlists.length === 0 && !playlistsLoading ? (
                <View style={styles.empty}>
                  <Text variant="label" tone="secondary">No playlists yet</Text>
                </View>
              ) : null
            }
          />
        </View>
      </Modal>

      <CreatePlaylistModal
        visible={createVisible}
        onClose={() => setCreateVisible(false)}
        onCreate={(name) => createMut.mutate(name)}
        loading={createMut.isPending}
      />
    </>
  );
}

const styles = StyleSheet.create({
  backdrop: { flex: 1, backgroundColor: 'rgba(0,0,0,0.5)' },
  sheet: {
    borderTopLeftRadius: 20,
    borderTopRightRadius: 20,
    paddingHorizontal: spacing.xl,
    paddingBottom: spacing['3xl'],
    paddingTop: spacing.md,
    maxHeight: '70%',
  },
  handle: {
    width: 36, height: 4, borderRadius: 2,
    alignSelf: 'center', marginBottom: spacing.lg,
  },
  sheetTitle: { marginBottom: spacing.xs },
  trackLabel: { marginBottom: spacing.lg },
  list: { flexGrow: 0 },
  playlistRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.md,
    paddingVertical: spacing.md,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  pressed: { opacity: 0.7 },
  createIcon: {
    width: 40, height: 40, borderRadius: 8,
    alignItems: 'center', justifyContent: 'center',
  },
  playlistIcon: {
    width: 40, height: 40, borderRadius: 8,
    alignItems: 'center', justifyContent: 'center',
  },
  playlistInfo: { flex: 1 },
  empty: { paddingTop: spacing.xl, alignItems: 'center' },
});
