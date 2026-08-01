import { useCallback, useEffect, useRef, useState, type ReactElement } from 'react';
import {
  ActivityIndicator,
  FlatList,
  type ListRenderItemInfo,
  Modal,
  Pressable,
  StyleSheet,
  View,
} from 'react-native';
import { useQuery } from '@tanstack/react-query';

import { getPlaylists } from '@shared/api-client/playlists';
import type { PlaylistResponse } from '@shared/api-client/types';
import { playlistKeys } from '@shared/lib/query-keys';
import { Text, spacing, useTheme } from '@shared/ui';

import { CreatePlaylistModal } from './CreatePlaylistModal';
import { useAddTracksToPlaylist, useCreatePlaylistWithTracks } from './mutations';

type AddToPlaylistSheetProps = {
  visible: boolean;
  label: string;
  resolveTrackIds: () => Promise<string[]>;
  onClose: () => void;
};

export function AddToPlaylistSheet({
  visible,
  label,
  resolveTrackIds,
  onClose,
}: AddToPlaylistSheetProps): ReactElement {
  const theme = useTheme();
  const [createVisible, setCreateVisible] = useState(false);
  const [addedTo, setAddedTo] = useState<string | null>(null);
  const [resolving, setResolving] = useState(false);
  const closeTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const clearCloseTimer = useCallback(() => {
    if (closeTimer.current) {
      clearTimeout(closeTimer.current);
      closeTimer.current = null;
    }
  }, []);
  useEffect(() => clearCloseTimer, [clearCloseTimer]);

  const { data: playlistsData, isLoading: playlistsLoading } = useQuery({
    queryKey: playlistKeys.list,
    queryFn: getPlaylists,
    enabled: visible,
    staleTime: Infinity,
  });

  const addMut = useAddTracksToPlaylist();
  const createMut = useCreatePlaylistWithTracks();
  const busy = resolving || addMut.isPending || createMut.isPending;

  const withTrackIds = useCallback(
    async (run: (trackIds: string[]) => void): Promise<void> => {
      setResolving(true);
      try {
        const trackIds = await resolveTrackIds();
        if (trackIds.length > 0) run(trackIds);
      } catch {
        onClose();
      } finally {
        setResolving(false);
      }
    },
    [onClose, resolveTrackIds],
  );

  const addToPlaylist = useCallback(
    (playlistId: string): void => {
      void withTrackIds((trackIds) =>
        addMut.mutate(
          { playlistId, trackIds },
          {
            onSuccess: () => {
              clearCloseTimer();
              setAddedTo(playlistId);
              closeTimer.current = setTimeout(() => {
                closeTimer.current = null;
                setAddedTo(null);
                onClose();
              }, 700);
            },
          },
        ),
      );
    },
    [addMut, clearCloseTimer, onClose, withTrackIds],
  );

  const createAndAdd = (name: string): void => {
    void withTrackIds((trackIds) =>
      createMut.mutate(
        { name, trackIds },
        {
          onSuccess: () => {
            setCreateVisible(false);
            onClose();
          },
        },
      ),
    );
  };

  const playlists = playlistsData?.items ?? [];

  const renderPlaylistItem = useCallback(
    ({ item }: ListRenderItemInfo<PlaylistResponse>) => (
      <Pressable
        testID={`add-to-playlist-${item.id}`}
        onPress={() => addToPlaylist(item.id)}
        disabled={busy}
        accessibilityRole="button"
        accessibilityLabel={`Add to ${item.name}, ${item.track_count} ${item.track_count === 1 ? 'track' : 'tracks'}`}
        accessibilityState={{ disabled: busy }}
        style={({ pressed }) => [
          styles.playlistRow,
          { borderBottomColor: theme.color.border },
          pressed ? styles.pressed : null,
        ]}
      >
        <View style={[styles.playlistIcon, { backgroundColor: theme.color.surface2 }]}>
          <Text variant="caption" tone="tertiary">
            ♫
          </Text>
        </View>
        <View style={styles.playlistInfo}>
          <Text variant="body" numberOfLines={1}>
            {item.name}
          </Text>
          <Text variant="caption" tone="secondary">
            {item.track_count} {item.track_count === 1 ? 'track' : 'tracks'}
          </Text>
        </View>
        {addedTo === item.id ? (
          <Text variant="caption" style={{ color: theme.color.success }}>
            Added ✓
          </Text>
        ) : null}
      </Pressable>
    ),
    [addToPlaylist, addedTo, busy, theme.color.border, theme.color.surface2, theme.color.success],
  );

  const handleClose = () => {
    clearCloseTimer();
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
        <Pressable
          style={[styles.backdrop, { backgroundColor: theme.color.scrim }]}
          onPress={handleClose}
          accessibilityRole="button"
          accessibilityLabel="Close"
        >
          <View />
        </Pressable>
        <View style={[styles.sheet, { backgroundColor: theme.color.surface1 }]}>
          <View style={[styles.handle, { backgroundColor: theme.color.border }]} />
          <Text variant="title" style={styles.sheetTitle}>
            Add to Playlist
          </Text>
          <View style={styles.subtitle}>
            <Text variant="caption" tone="secondary" numberOfLines={1} style={styles.trackLabel}>
              {label}
            </Text>
            {busy ? <ActivityIndicator size="small" testID="add-to-playlist-busy" /> : null}
          </View>

          <Pressable
            testID="add-to-playlist-create-new"
            onPress={() => setCreateVisible(true)}
            disabled={busy}
            accessibilityRole="button"
            accessibilityLabel="Create new playlist"
            style={({ pressed }) => [
              styles.playlistRow,
              { borderBottomColor: theme.color.border },
              pressed ? styles.pressed : null,
            ]}
          >
            <View style={[styles.createIcon, { backgroundColor: theme.color.accent }]}>
              <Text variant="bodyStrong" tone="onAccent">
                +
              </Text>
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
                  <Text variant="label" tone="secondary">
                    No playlists yet
                  </Text>
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
        loading={busy}
      />
    </>
  );
}

const styles = StyleSheet.create({
  backdrop: { flex: 1 },
  sheet: {
    borderTopLeftRadius: 20,
    borderTopRightRadius: 20,
    paddingHorizontal: spacing.xl,
    paddingBottom: spacing['3xl'],
    paddingTop: spacing.md,
    maxHeight: '70%',
  },
  handle: {
    width: 36,
    height: 4,
    borderRadius: 2,
    alignSelf: 'center',
    marginBottom: spacing.lg,
  },
  sheetTitle: { marginBottom: spacing.xs },
  subtitle: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.sm,
    marginBottom: spacing.lg,
  },
  trackLabel: { flex: 1 },
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
    width: 40,
    height: 40,
    borderRadius: 8,
    alignItems: 'center',
    justifyContent: 'center',
  },
  playlistIcon: {
    width: 40,
    height: 40,
    borderRadius: 8,
    alignItems: 'center',
    justifyContent: 'center',
  },
  playlistInfo: { flex: 1 },
  empty: { paddingTop: spacing.xl, alignItems: 'center' },
});
