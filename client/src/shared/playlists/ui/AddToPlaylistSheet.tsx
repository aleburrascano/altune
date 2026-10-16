import { useCallback, useState, type ReactElement } from 'react';
import {
  ActivityIndicator,
  FlatList,
  type ListRenderItemInfo,
  StyleSheet,
  View,
} from 'react-native';
import { useQuery } from '@tanstack/react-query';

import type { PlaylistId, TrackId } from '@shared/apiClient/ids';
import type { PlaylistResponse } from '@shared/apiClient/playlistResponse';
import { countLabel } from '@shared/ui/displayFormat';
import { playlistKeys } from '@shared/query/queryKeys';
import { Text, pressedStyle, spacing, useTheme } from '@shared/ui';
import { ModalFrame } from '@shared/ui/primitives/ModalFrame';
import { Tappable } from '@shared/ui/primitives/Tappable';

import { CreatePlaylistModal } from '@shared/playlists/ui/CreatePlaylistModal';
import { usePlaylistsPort } from '@shared/playlists/ports/PlaylistsPortProvider';
import {
  useAddTracksToPlaylist,
  useCreatePlaylistWithTracks,
} from '@shared/playlists/hooks/playlist/mutations';
import { useSingleFlightAction } from '@shared/playlists/hooks/playlist/useSingleFlightAction';
import { radius, sheetGrabber, sheetTopRadius } from '@shared/ui/theme';

const SHEET_PLAYLIST_CAP = 2000;

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

  const port = usePlaylistsPort();
  const { data: playlistsData, isLoading: playlistsLoading } = useQuery({
    queryKey: playlistKeys.list,
    queryFn: ({ signal }) => port.getPlaylists({ limit: SHEET_PLAYLIST_CAP }, signal),
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
              closeAfter(ADDED_CONFIRMATION_MS, () => setAddedTo(null));
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
      <Tappable
        action="playlists.add_to_playlist.pick"
        testID={`add-to-playlist-${item.id}`}
        onPress={() => addToPlaylist(item.id)}
        disabled={busy}
        accessibilityRole="button"
        accessibilityLabel={`Add to ${item.name}, ${item.track_count} ${countLabel(item.track_count, 'track')}`}
        accessibilityState={{ disabled: busy }}
        style={({ pressed }) => [
          styles.playlistRow,
          { borderBottomColor: theme.color.border },
          pressedStyle(pressed),
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
      </Tappable>
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
      <ModalFrame
        testID="add-to-playlist-sheet"
        visible={visible && !createVisible}
        variant="sheet"
        onClose={handleClose}
        closeAction="playlists.add_to_playlist.close"
      >
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

          <Tappable
            action="playlists.add_to_playlist.create_new"
            testID="add-to-playlist-create-new"
            onPress={() => setCreateVisible(true)}
            disabled={busy}
            accessibilityRole="button"
            accessibilityLabel="Create new playlist"
            style={({ pressed }) => [
              styles.playlistRow,
              { borderBottomColor: theme.color.border },
              pressedStyle(pressed),
            ]}
          >
            <View style={[styles.createIcon, { backgroundColor: theme.color.accent }]}>
              <Text variant="bodyStrong" tone="onAccent">
                +
              </Text>
            </View>
            <Text variant="bodyStrong">Create New Playlist</Text>
          </Tappable>

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
      </ModalFrame>

      <CreatePlaylistModal
        visible={createVisible}
        onClose={() => setCreateVisible(false)}
        onCreate={createAndAdd}
        loading={busy}
      />
    </>
  );
}

const ADDED_CONFIRMATION_MS = 700;
const PLAYLIST_ICON_SIZE = 40;

const styles = StyleSheet.create({
  sheet: {
    borderTopLeftRadius: sheetTopRadius,
    borderTopRightRadius: sheetTopRadius,
    paddingHorizontal: spacing.xl,
    paddingBottom: spacing['3xl'],
    paddingTop: spacing.md,
    maxHeight: '70%',
  },
  handle: {
    ...sheetGrabber,
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
  createIcon: {
    width: PLAYLIST_ICON_SIZE,
    height: PLAYLIST_ICON_SIZE,
    borderRadius: radius.sm,
    alignItems: 'center',
    justifyContent: 'center',
  },
  playlistIcon: {
    width: PLAYLIST_ICON_SIZE,
    height: PLAYLIST_ICON_SIZE,
    borderRadius: radius.sm,
    alignItems: 'center',
    justifyContent: 'center',
  },
  playlistInfo: { flex: 1 },
  empty: { paddingTop: spacing.xl, alignItems: 'center' },
});
