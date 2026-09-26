import type { ReactElement } from 'react';
import { FlatList, Pressable, StyleSheet } from 'react-native';
import { Shuffle } from 'lucide-react-native';

import type { TrackId } from '@shared/api-client/ids';
import type { TrackResponse } from '@shared/api-client/types';
import { Text, spacing, useTheme } from '@shared/ui';
import type { MenuAnchor } from '@shared/ui/primitives/menuPlacement';

import type { Selection } from '../hooks/useSelection';
import { LibraryEmptyMessage } from './LibraryEmptyMessage';
import { ListLoadingMoreFooter } from './ListLoadingMoreFooter';
import { LibraryRow } from './LibraryRow';
import { listContent } from './listContentStyles';
import type { ListPaging, ListRefresh } from '../refresh';

type TracksListProps = {
  tracks: TrackResponse[];
  emptyLabel: string;
  refresh: ListRefresh;
  onPlay: (track: TrackResponse) => void;
  onPress: (track: TrackResponse) => void;
  onMore: (track: TrackResponse, anchor: MenuAnchor) => void;
  onRetry: (track: TrackResponse) => void;
  isRetrying: (trackId: TrackId) => boolean;
  isPlaying: (trackId: TrackId) => boolean;
  paging?: ListPaging;
  onShuffleAll?: () => void;
  selection?: Selection;
};

function ShuffleAllButton({ onPress }: { onPress: () => void }): ReactElement {
  const theme = useTheme();
  return (
    <Pressable
      testID="library-shuffle-all"
      onPress={onPress}
      style={({ pressed }) => [
        styles.shuffleAll,
        { backgroundColor: theme.color.surface1, borderColor: theme.color.border },
        pressed ? styles.pressed : null,
      ]}
      accessibilityRole="button"
      accessibilityLabel="Shuffle whole library"
      accessibilityHint="Plays every track in your library in random order"
    >
      <Shuffle size={16} color={theme.color.textPrimary} />
      <Text variant="label">Shuffle all</Text>
    </Pressable>
  );
}

export function TracksList({
  tracks,
  emptyLabel,
  refresh,
  onPlay,
  onPress,
  onMore,
  onRetry,
  isRetrying,
  isPlaying,
  paging,
  onShuffleAll,
  selection,
}: TracksListProps): ReactElement {
  return (
    <FlatList
      testID="library-tracks-list"
      data={tracks}
      keyExtractor={(t) => t.id}
      showsVerticalScrollIndicator={false}
      onRefresh={refresh.onRefresh}
      refreshing={refresh.refreshing}
      onEndReached={paging?.onEndReached}
      onEndReachedThreshold={0.5}
      ListHeaderComponent={
        onShuffleAll != null && tracks.length > 0 ? (
          <ShuffleAllButton onPress={onShuffleAll} />
        ) : null
      }
      ListFooterComponent={
        <ListLoadingMoreFooter
          loading={paging?.isFetchingNextPage === true}
          failed={paging?.nextPageFailed === true}
          onRetry={paging?.onRetryNextPage}
        />
      }
      contentContainerStyle={tracks.length === 0 ? listContent.empty : listContent.padded}
      ListEmptyComponent={<LibraryEmptyMessage label={emptyLabel} />}
      renderItem={({ item }) => (
        <LibraryRow
          track={item}
          {...(item.acquisition_status === 'ready' ? { onPlay: () => onPlay(item) } : {})}
          onPress={() => onPress(item)}
          onMore={(anchor) => onMore(item, anchor)}
          {...(selection != null ? { onLongPress: () => selection.begin(item.id) } : {})}
          {...(selection?.active
            ? {
                selectable: {
                  selected: selection.has(item.id),
                  onToggle: () => selection.toggle(item.id),
                },
              }
            : {})}
          {...(item.acquisition_status === 'failed' ? { onRetry: () => onRetry(item) } : {})}
          retrying={isRetrying(item.id)}
          isPlaying={isPlaying(item.id)}
        />
      )}
    />
  );
}

const styles = StyleSheet.create({
  shuffleAll: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: spacing.xs,
    paddingVertical: spacing.sm + 2,
    marginBottom: spacing.sm,
    borderRadius: 999,
    borderWidth: 1,
  },
  pressed: { opacity: 0.7 },
});
