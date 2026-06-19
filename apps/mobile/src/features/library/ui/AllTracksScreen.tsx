import { useState, type ReactElement } from 'react';
import { FlatList, StyleSheet, View } from 'react-native';
import { useRouter } from 'expo-router';

import type { TrackResponse } from '@shared/api-client/types';
import { isCurrentlyPlaying } from '@shared/playback/isCurrentlyPlaying';
import { buildPlayableQueue } from '@shared/playback/playFromList';
import { usePlayback } from '@shared/playback/usePlayback';
import { useQueuePlayback } from '@shared/playback/useQueuePlayback';
import { Screen, SearchBar, Text, spacing, useTheme } from '@shared/ui';
import { ActionSheet } from '@shared/ui/primitives/ActionSheet';

import { useDeleteTrack } from '../hooks/useDeleteTrack';
import { useLibraryHome } from '../hooks/useLibraryHome';
import { useLibrarySearch } from '../hooks/useLibrarySearch';
import { useRetryAcquisition } from '../hooks/useRetryAcquisition';
import { useLibraryNavigation } from './useLibraryNavigation';
import { AddToPlaylistSheet } from './AddToPlaylistSheet';
import { ExpandedHeader } from './ExpandedHeader';
import { LibraryRow } from './LibraryRow';
import { sortTracks, TRACK_SORT_OPTIONS } from './sort';
import type { SortKey } from './sort';

export function AllTracksScreen(): ReactElement {
  const router = useRouter();
  const theme = useTheme();
  const state = useLibraryHome();
  const { navigateToTrack } = useLibraryNavigation(router);
  const retryMutation = useRetryAcquisition();
  const retryingTrackId = retryMutation.isPending ? retryMutation.variables : undefined;
  const deleteMutation = useDeleteTrack();
  const playback = usePlayback();
  const queue = useQueuePlayback();
  const search = useLibrarySearch();
  const [sortKey, setSortKey] = useState<SortKey>('recent');
  const [addToPlaylistTrack, setAddToPlaylistTrack] = useState<TrackResponse | null>(null);
  const [actionTrack, setActionTrack] = useState<TrackResponse | null>(null);
  const [searchFocused, setSearchFocused] = useState(false);

  const sorted = sortTracks(state.allTracks, sortKey);
  const filtered = search.filter(sorted);

  return (
    <Screen>
      <ExpandedHeader
        title="Recently Added"
        onBack={() => router.back()}
        sortKey={sortKey}
        onSortChange={setSortKey}
        sortOptions={TRACK_SORT_OPTIONS}
      />
      <SearchBar
        value={search.inputValue}
        onChangeText={search.onChangeText}
        onSubmitEditing={search.onSubmit}
        onClear={search.onClear}
        onFocus={() => setSearchFocused(true)}
        onBlur={() => setSearchFocused(false)}
        focused={searchFocused}
        placeholder="Search library"
        testID="library-search-input"
        theme={theme}
      />
      {search.hasQuery && filtered.length === 0 ? (
        <View style={styles.emptySearch}>
          <Text variant="body" tone="secondary">No tracks found</Text>
        </View>
      ) : (
      <FlatList
        data={filtered}
        keyExtractor={(t) => t.id}
        renderItem={({ item }) => (
          <LibraryRow
            track={item}
            {...(item.acquisition_status === 'ready' ? { onPlay: () => {
              const { playable, startIndex } = buildPlayableQueue(filtered, item.id);
              queue.playFromList(playable, startIndex, { kind: 'library' });
            } } : {})}
            onPress={() => navigateToTrack(item)}
            onMore={() => setActionTrack(item)}
            {...(item.acquisition_status === 'failed' ? { onRetry: () => retryMutation.mutate(item.id) } : {})}
            retrying={retryingTrackId === item.id}
            isPlaying={isCurrentlyPlaying(playback, { kind: 'library', trackId: item.id })}
          />
        )}
        showsVerticalScrollIndicator={false}
        contentContainerStyle={styles.list}
      />
      )}
      <AddToPlaylistSheet
        visible={addToPlaylistTrack != null}
        trackId={addToPlaylistTrack?.id ?? ''}
        trackTitle={addToPlaylistTrack != null ? `${addToPlaylistTrack.title} — ${addToPlaylistTrack.artist}` : ''}
        onClose={() => setAddToPlaylistTrack(null)}
      />
      <ActionSheet
        visible={actionTrack != null}
        title={actionTrack?.title}
        subtitle={actionTrack != null ? `${actionTrack.artist}${actionTrack.album != null ? ` · ${actionTrack.album}` : ''}` : undefined}
        options={actionTrack != null ? [
          { label: 'View Details', onPress: () => navigateToTrack(actionTrack) },
          { label: 'Add to Playlist', onPress: () => setAddToPlaylistTrack(actionTrack) },
          { label: 'Remove from Library', tone: 'danger' as const, onPress: () => deleteMutation.mutate(actionTrack.id) },
        ] : []}
        onClose={() => setActionTrack(null)}
      />
    </Screen>
  );
}

const styles = StyleSheet.create({
  list: { paddingBottom: spacing['3xl'] },
  emptySearch: { flex: 1, alignItems: 'center', paddingTop: spacing['3xl'] },
});
