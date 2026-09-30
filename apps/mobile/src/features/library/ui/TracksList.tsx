import type { ReactElement } from 'react';
import { ActivityIndicator, FlatList, Pressable, StyleSheet } from 'react-native';
import { Shuffle } from 'lucide-react-native';

import type { TrackId } from '@shared/api-client/ids';
import type { TrackResponse } from '@shared/api-client/types';
import { Text, pressedStyle, spacing, useWideWebLayout, useTheme } from '@shared/ui';
import type { MenuAnchor } from '@shared/ui/primitives/menuPlacement';

import type { Selection } from '../hooks/useSelection';
import { LibraryEmptyMessage } from './LibraryEmptyMessage';
import { ListLoadingMoreFooter } from './ListLoadingMoreFooter';
import { LibraryRow } from './LibraryRow';
import { listContent } from './listContentStyles';
import { WideTrackHeader } from './WideTrackHeader';
import type { ListPaging, ListRefresh } from '../refresh';

type TracksListHeaderProps = {
  isWide: boolean;
  showShuffle: boolean;
  onShuffleAll: (() => void) | undefined;
  shuffleBusy: boolean;
};

function TracksListHeader(props: TracksListHeaderProps): ReactElement {
  const { isWide, showShuffle, onShuffleAll, shuffleBusy } = props;
  return (
    <>
      {showShuffle && onShuffleAll != null ? (
        <ShuffleAllButton onPress={onShuffleAll} busy={shuffleBusy} />
      ) : null}
      {isWide && showShuffle ? <WideTrackHeader /> : null}
    </>
  );
}

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
  shuffleBusy?: boolean;
  selection?: Selection;
};

function ShuffleAllIcon({ busy }: { busy: boolean }): ReactElement {
  const theme = useTheme();
  if (busy) return <ActivityIndicator testID="library-shuffle-all-busy" size="small" />;
  return <Shuffle size={16} color={theme.color.textPrimary} />;
}

const shuffleAllStatic = {
  testID: 'library-shuffle-all',
  accessibilityRole: 'button',
  accessibilityLabel: 'Shuffle whole library',
  accessibilityHint: 'Plays every track in your library in random order',
} as const;

function shuffleAllProps(onPress: () => void, busy: boolean) {
  return { ...shuffleAllStatic, onPress, disabled: busy, accessibilityState: { busy } };
}

function useShuffleAllStyle() {
  const theme = useTheme();
  const colors = { backgroundColor: theme.color.surface1, borderColor: theme.color.border };
  return ({ pressed }: { pressed: boolean }) => [styles.shuffleAll, colors, pressedStyle(pressed)];
}

function ShuffleAllButton({ onPress, busy }: { onPress: () => void; busy: boolean }): ReactElement {
  const style = useShuffleAllStyle();
  return (
    <Pressable {...shuffleAllProps(onPress, busy)} style={style}>
      <ShuffleAllIcon busy={busy} />
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
  shuffleBusy = false,
  selection,
}: TracksListProps): ReactElement {
  const isWide = useWideWebLayout();
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
        <TracksListHeader
          isWide={isWide}
          showShuffle={tracks.length > 0}
          onShuffleAll={onShuffleAll}
          shuffleBusy={shuffleBusy}
        />
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
});
