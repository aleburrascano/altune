import { useRouter } from 'expo-router';
import { useState, type ReactElement } from 'react';
import { Alert, StyleSheet, View } from 'react-native';

import { asTrackId } from '@shared/api-client/ids';
import type { TrackResponse } from '@shared/api-client/types';
import { describeError } from '@shared/lib/describeError';
import { countLabel } from '@shared/lib/format';
import { usePlayback } from '@shared/playback/usePlayback';
import { useQueuePlayback } from '@shared/playback/useQueuePlayback';
import { Button, Screen, Skeleton, Text, spacing, useTheme } from '@shared/ui';
import { useAnnounceChange } from '@shared/ui/useAnnounceChange';
import { ContextMenu } from '@shared/ui/primitives/ContextMenu';
import { SearchBar } from '@shared/ui/primitives/SearchBar';

import { AddToPlaylistSheet, CreatePlaylistModal } from '@shared/playlists';
import { usePinnedStore } from '@shared/offline/pinnedStore';

import { useActiveLibraryView } from '../hooks/useActiveLibraryView';
import { useDeleteTrack, useDeleteTracks } from '../hooks/useDeleteTrack';
import { useLibraryIsEmpty } from '../hooks/useLibraryHome';
import { useLibrarySearch } from '../hooks/useLibrarySearch';
import { usePlaylistActions } from '../hooks/usePlaylistActions';
import { useRetryAcquisition } from '../hooks/useRetryAcquisition';
import { _viewForState } from '../state';
import { useSelection } from '../useSelection';
import { LibraryChips, type LibraryChip } from './LibraryChips';
import { SelectionBar } from './SelectionBar';
import { buildSelectionActions } from './selectionActions';
import { LibraryHeader } from './LibraryHeader';
import { LibraryNoResults } from './LibraryNoResults';
import type { MenuAnchor } from '@shared/ui/primitives/menuPlacement';
import { SortControl } from './SortControl';
import { useReacquireTrack } from '../hooks/useReacquireTrack';
import { buildTrackMenuItems } from './trackMenu';
import { type SortKey } from './sort';
import { useLibraryNavigation } from './useLibraryNavigation';

const DEFAULT_SORTS: Record<LibraryChip, SortKey> = {
  playlists: 'recent',
  tracks: 'recent',
  albums: 'az',
  artists: 'az',
};

export function LibraryScreen(): ReactElement {
  const router = useRouter();
  const theme = useTheme();
  const pl = usePlaylistActions();
  const search = useLibrarySearch();
  const navigation = useLibraryNavigation(router);
  const deleteMutation = useDeleteTrack();
  const deleteManyMutation = useDeleteTracks();
  const retryMutation = useRetryAcquisition();
  const reacquireMutation = useReacquireTrack();
  const playback = usePlayback();
  const queue = useQueuePlayback();
  const selection = useSelection();
  const pinnedEntries = usePinnedStore((s) => s.entries);
  const pinMany = usePinnedStore((s) => s.pinMany);
  const unpin = usePinnedStore((s) => s.unpin);

  const [chip, setChip] = useState<LibraryChip>('playlists');
  const [sortByChip, setSortByChip] = useState<Record<LibraryChip, SortKey>>(DEFAULT_SORTS);
  const [action, setAction] = useState<{ track: TrackResponse; anchor: MenuAnchor } | null>(null);
  const [searchFocused, setSearchFocused] = useState(false);
  const [bulkPlaylistVisible, setBulkPlaylistVisible] = useState(false);

  const libraryIsEmpty = useLibraryIsEmpty();

  const { active, tracks, playlists } = useActiveLibraryView(chip, sortByChip, search.query, {
    pl,
    router,
    navigation,
    selection,
    queue,
    playback,
    retryMutation,
    onTrackMore: (track, anchor) => setAction({ track, anchor }),
  });

  const confirmRemoveTrack = (track: TrackResponse): void => {
    Alert.alert('Remove from Library', `Remove "${track.title}" from your library?`, [
      { text: 'Cancel', style: 'cancel' },
      { text: 'Remove', style: 'destructive', onPress: () => deleteMutation.mutate(track.id) },
    ]);
  };

  const trackMenuItems = (track: TrackResponse) =>
    buildTrackMenuItems(track, {
      onReacquire: () => reacquireMutation.mutate(track.id),
      queue,
      onViewDetails: () => navigation.navigateToTrack(track),
      onAddToPlaylist: () => pl.setAddToPlaylistTrack(track),
      danger: { label: 'Remove from Library', onPress: () => confirmRemoveTrack(track) },
    });

  const confirmDeleteSelected = (): void => {
    const ids = selection.ids;
    Alert.alert(
      'Remove from Library',
      `Remove ${ids.length} ${countLabel(ids.length, 'track')} from your library?`,
      [
        { text: 'Cancel', style: 'cancel' },
        {
          text: 'Remove',
          style: 'destructive',
          onPress: () => {
            deleteManyMutation.mutate(ids);
            selection.clear();
          },
        },
      ],
    );
  };

  const selectionActions = buildSelectionActions(
    tracks.filter((t) => selection.has(t.id)),
    {
      pinnedEntries,
      pinMany,
      unpin,
      queue,
      onAddToPlaylist: () => setBulkPlaylistVisible(true),
      onDone: selection.clear,
      danger: { label: 'Remove', onPress: confirmDeleteSelected },
    },
  );

  const sortKey = sortByChip[chip];
  const setSort = (key: SortKey): void => setSortByChip((prev) => ({ ...prev, [chip]: key }));

  useAnnounceChange(
    search.hasQuery ? `${active.count} ${countLabel(active.count, 'result')}` : '',
  );

  const view = _viewForState({
    isLoading: active.isLoading,
    error: active.error,
    items: active.count === 0 ? [] : [active.count],
  });

  if (view === 'loading') {
    return (
      <Screen>
        <LibraryHeader />
        <View testID="library-loading" style={styles.skeletonGrid}>
          {[0, 1, 2, 3].map((i) => (
            <Skeleton key={i} width="47%" height={140} radius={8} />
          ))}
        </View>
      </Screen>
    );
  }

  if (view === 'error') {
    const { title, body } = describeError(active.error);
    return (
      <Screen>
        <LibraryHeader />
        <View testID="library-error" style={styles.center}>
          <Text variant="title">{title}</Text>
          <Text variant="label" tone="secondary" style={styles.centerSub}>
            {body}
          </Text>
          <Button testID="library-retry" label="Retry" onPress={active.onRetry} />
        </View>
      </Screen>
    );
  }

  if (view === 'empty' && !search.hasQuery && playlists.length === 0 && libraryIsEmpty) {
    return (
      <Screen>
        <LibraryHeader />
        <View testID="library-empty" style={styles.center}>
          <Text variant="title">Your library is empty</Text>
          <Text variant="label" tone="secondary" style={styles.centerSub}>
            Tracks you add will show up here.
          </Text>
          <Button label="Discover Music" onPress={() => router.push('/discover')} />
        </View>
      </Screen>
    );
  }

  return (
    <Screen>
      <LibraryHeader />
      <SearchBar
        value={search.inputValue}
        onChangeText={search.onChangeText}
        onSubmitEditing={search.onSubmit}
        onClear={search.onClear}
        onFocus={() => setSearchFocused(true)}
        onBlur={() => setSearchFocused(false)}
        focused={searchFocused}
        placeholder="Search your library"
        testID="library-search-input"
        theme={theme}
      />
      <LibraryChips value={chip} onChange={setChip} />
      <SortControl
        count={active.count}
        noun={active.noun}
        sortKey={sortKey}
        options={active.options}
        onSortChange={setSort}
      />
      <View style={styles.body}>
        {search.hasQuery && active.count === 0 ? (
          <LibraryNoResults query={search.query} onClear={search.onClear} />
        ) : (
          active.content
        )}
      </View>

      {selection.active && chip === 'tracks' ? (
        <SelectionBar
          count={selection.count}
          allSelected={selection.count === tracks.length}
          onSelectAll={() =>
            selection.count === tracks.length
              ? selection.clear()
              : selection.selectAll(tracks.map((t) => t.id))
          }
          onCancel={selection.clear}
          actions={selectionActions}
        />
      ) : null}

      <CreatePlaylistModal
        visible={pl.createModalVisible}
        onClose={() => pl.setCreateModalVisible(false)}
        onCreate={pl.createPlaylist}
        loading={pl.createLoading}
      />
      <AddToPlaylistSheet
        visible={pl.addToPlaylistTrack != null}
        label={
          pl.addToPlaylistTrack != null
            ? `${pl.addToPlaylistTrack.title} — ${pl.addToPlaylistTrack.artist}`
            : ''
        }
        resolveTrackIds={() => Promise.resolve([pl.addToPlaylistTrack?.id ?? asTrackId('')])}
        onClose={() => pl.setAddToPlaylistTrack(null)}
      />
      <AddToPlaylistSheet
        visible={bulkPlaylistVisible}
        label={`${selection.count} ${countLabel(selection.count, 'track')}`}
        resolveTrackIds={() => Promise.resolve(selection.ids)}
        onClose={() => {
          setBulkPlaylistVisible(false);
          selection.clear();
        }}
      />
      <ContextMenu
        visible={action != null}
        anchor={action?.anchor}
        items={action != null ? trackMenuItems(action.track) : []}
        onClose={() => setAction(null)}
      />
    </Screen>
  );
}

const styles = StyleSheet.create({
  body: { flex: 1 },
  center: { flex: 1, alignItems: 'center', justifyContent: 'center', padding: spacing['2xl'] },
  centerSub: { marginTop: spacing.xs, marginBottom: spacing.lg, textAlign: 'center' },
  skeletonGrid: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: spacing.md,
    paddingTop: spacing.xl,
  },
});
