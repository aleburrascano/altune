import { useRouter } from 'expo-router';
import { useState, type ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { countLabel } from '@shared/lib/format';
import { usePlayback } from '@shared/playback/usePlayback';
import { useQueuePlayback } from '@shared/playback/useQueuePlayback';
import { Screen, useTheme } from '@shared/ui';
import { AsyncSection } from '@shared/ui/AsyncSection';
import { useAnnounceChange } from '@shared/ui/useAnnounceChange';
import { SearchBar } from '@shared/ui/primitives/SearchBar';

import { AddToPlaylistSheet, CreatePlaylistModal } from '@shared/playlists';

import { useActiveLibraryView } from '../hooks/useActiveLibraryView';
import { useLibraryIsEmpty } from '../hooks/useLibraryIsEmpty';
import { useLibrarySearch } from '../hooks/useLibrarySearch';
import { useLibraryTrackSelection } from '../hooks/useLibraryTrackSelection';
import { usePlaylistActions } from '../hooks/usePlaylistActions';
import { useRetryAcquisition } from '../hooks/useRetryAcquisition';
import { librarySection } from '../state';
import { LibraryChips } from './LibraryChips';
import { LibraryHeader } from './LibraryHeader';
import { LibraryNoResults } from './LibraryNoResults';
import {
  LibraryEmptyScreen,
  LibraryErrorScreen,
  LibrarySkeletonScreen,
} from './LibraryStateScreens';
import { SortControl } from './SortControl';
import { TrackSelectionOverlay } from './TrackSelectionOverlay';
import type { LibraryChip } from '../activeView';
import { type SortKey } from '../sort';
import { useLibraryNavigation } from '../hooks/useLibraryNavigation';

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
  const retryMutation = useRetryAcquisition('library_row');
  const playback = usePlayback();
  const queue = useQueuePlayback();

  const [chip, setChip] = useState<LibraryChip>('playlists');
  const [sortByChip, setSortByChip] = useState<Record<LibraryChip, SortKey>>(DEFAULT_SORTS);
  const [searchFocused, setSearchFocused] = useState(false);

  const libraryIsEmpty = useLibraryIsEmpty();

  const trackSelection = useLibraryTrackSelection({ queue, navigation, pl });

  const { active, tracks, playlists } = useActiveLibraryView(chip, sortByChip, search.query, {
    pl,
    navigation,
    selection: trackSelection.selection,
    queue,
    playback,
    retryMutation,
    onTrackMore: trackSelection.onTrackMore,
  });

  const sortKey = sortByChip[chip];
  const setSort = (key: SortKey): void => setSortByChip((prev) => ({ ...prev, [chip]: key }));

  useAnnounceChange(search.hasQuery ? `${active.count} ${countLabel(active.count, 'result')}` : '');

  const section = librarySection({
    isLoading: active.isLoading,
    error: active.error,
    count: active.count,
    showEmpty: !search.hasQuery && playlists.length === 0 && libraryIsEmpty,
  });

  return (
    <AsyncSection
      view={section}
      skeleton={() => <LibrarySkeletonScreen />}
      error={() => <LibraryErrorScreen error={active.error} onRetry={active.onRetry} />}
      empty={() => <LibraryEmptyScreen onDiscover={() => router.push('/discover')} />}
    >
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
          resolveTrackIds={() =>
            Promise.resolve(pl.addToPlaylistTrack != null ? [pl.addToPlaylistTrack.id] : [])
          }
          onClose={() => pl.setAddToPlaylistTrack(null)}
        />

        <TrackSelectionOverlay
          controller={trackSelection}
          tracks={tracks}
          barVisible={trackSelection.selection.active && chip === 'tracks'}
        />
      </Screen>
    </AsyncSection>
  );
}

const styles = StyleSheet.create({
  body: { flex: 1 },
});
