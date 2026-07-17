import { useCallback, useState, type ReactElement } from 'react';
import { FlatList, type ListRenderItemInfo, Modal, Pressable, StyleSheet, View } from 'react-native';
import { useQuery } from '@tanstack/react-query';

import { getPlaylists } from '@shared/api-client/playlists';
import type { PlaylistResponse } from '@shared/api-client/types';
import { playlistKeys } from '@shared/lib/query-keys';
import { Text, spacing, useTheme } from '@shared/ui';

import { useAddTrackToPlaylist, useCreatePlaylistWithTrack } from '../hooks/usePlaylistMutations';
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
  const [createVisible, setCreateVisible] = useState(false);
  const [addedTo, setAddedTo] = useState<string | null>(null);

  const { data: playlistsData, isLoading: playlistsLoading } = useQuery({
    queryKey: playlistKeys.list,
    queryFn: getPlaylists,
    enabled: visible,
    staleTime: Infinity, // SSE-covered; event patches keep it fresh (F15)
  });

  // Cache policy (optimistic count bump, rollback, invalidate) lives in the
  // mutation hooks; this sheet keeps only its own visibility state.
  const addMut = useAddTrackToPlaylist(trackId);
  const createMut = useCreatePlaylistWithTrack(trackId);

  const addToPlaylist = useCallback(
    (playlistId: string): void => {
      addMut.mutate(playlistId, {
        onSuccess: () => setAddedTo(playlistId),
        onSettled: () => {
          setAddedTo(null);
          onClose();
        },
      });
    },
    [addMut, onClose],
  );

  const createAndAdd = (name: string): void => {
    createMut.mutate(name, {
      onSettled: () => {
        setCreateVisible(false);
        onClose();
      },
    });
  };

  const playlists = playlistsData?.items ?? [];

  const renderPlaylistItem = useCallback(
    ({ item }: ListRenderItemInfo<PlaylistResponse>) => (
      <Pressable
        testID={`add-to-playlist-${item.id}`}
        onPress={() => addToPlaylist(item.id)}
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
    [addMut.isPending, addToPlaylist, addedTo, theme.color.border, theme.color.surface2, theme.color.success],
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
        onCreate={createAndAdd}
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
