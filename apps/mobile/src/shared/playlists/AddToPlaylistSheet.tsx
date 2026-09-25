import { useCallback, useState, type ReactElement } from 'react';
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

import type { PlaylistId, TrackId } from '@shared/api-client/ids';
import { getPlaylists } from '@shared/api-client/playlists';
import type { PlaylistResponse } from '@shared/api-client/types';
import { countLabel } from '@shared/lib/format';
import { playlistKeys } from '@shared/lib/query-keys';
import { Text, spacing, useTheme } from '@shared/ui';

import { CreatePlaylistModal } from './CreatePlaylistModal';
import { useAddTracksToPlaylist, useCreatePlaylistWithTracks } from './mutations';
import { useSingleFlightAction } from './useSingleFlightAction';

// The sheet picks one playlist out of a single scroll, so it asks for the server's
// whole row cap rather than the short default page a caller naming no limit is
// served (#1708). A user past the cap cannot reach their oldest playlists here.
const SHEET_PLAYLIST_CAP = 2000;

// The sheet closes on a rejected resolve, which on screen is indistinguishable
// from the user dismissing it. Whoever owns `resolveTrackIds` owns the copy for
// its failure (the detail screen already banners a failed save), so the sheet
// owes the log rather than a second notice (#1783).
function reportUnresolvedTracks(error: unknown): void {
  console.warn('[playlists] could not resolve the tracks to add; closing the sheet', error);
}

type AddToPlaylistSheetProps = {
  visible: boolean;
  label: string;
  resolveTrackIds: () => Promise<TrackId[]>;
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
  const {
    resolving,
    run: withTrackIds,
    close,
    closeAfter,
    cancelScheduledClose,
  } = useSingleFlightAction({
    open: visible,
    resolve: resolveTrackIds,
    onResolveError: reportUnresolvedTracks,
    onClose,
  });

  const { data: playlistsData, isLoading: playlistsLoading } = useQuery({
    queryKey: playlistKeys.list,
    queryFn: ({ signal }) => getPlaylists({ limit: SHEET_PLAYLIST_CAP }, signal),
    enabled: visible,
    staleTime: Infinity,
  });

  const addMut = useAddTracksToPlaylist();
  const createMut = useCreatePlaylistWithTracks();
  const busy = resolving || addMut.isPending || createMut.isPending;

  const addToPlaylist = useCallback(
    (playlistId: PlaylistId): void => {
      void withTrackIds((trackIds) =>
        addMut.mutate(
          { playlistId, trackIds },
          {
            onSuccess: () => {
              setAddedTo(playlistId);
              closeAfter(700, () => setAddedTo(null));
            },
          },
        ),
      );
    },
    [addMut, closeAfter, withTrackIds],
  );

  const createAndAdd = (name: string): void => {
    void withTrackIds((trackIds) =>
      createMut.mutate(
        { name, trackIds },
        {
          onSuccess: () => {
            setCreateVisible(false);
            close();
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
        accessibilityLabel={`Add to ${item.name}, ${item.track_count} ${countLabel(item.track_count, 'track')}`}
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
            {item.track_count} {countLabel(item.track_count, 'track')}
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
    cancelScheduledClose();
    setAddedTo(null);
    close();
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
