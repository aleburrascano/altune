import { memo, useCallback, useMemo, type ReactElement } from 'react';
import {
  ActivityIndicator,
  FlatList,
  Pressable,
  StyleSheet,
  type ListRenderItem,
} from 'react-native';
import { Shuffle } from 'lucide-react-native';
import type { TrackId } from '@shared/apiClient/ids';
import type { TrackResponse } from '@shared/apiClient/trackResponse';
import { Text, pressedStyle, spacing, useWideWebLayout, useTheme } from '@shared/ui';
import type { MenuAnchor } from '@shared/ui/primitives/menuPlacement';
import type { Selection } from '@features/library/hooks/useSelection';
import { LibraryEmptyMessage, ListLoadingMoreFooter } from '@features/library/ui/libraryListParts';
import { listContent } from '@features/library/ui/listContentStyles';
import { LibraryRow } from '@features/library/ui/LibraryRow';
import { WideTrackHeader } from '@features/library/ui/WideTrackHeader';
import type { ListPaging, ListRefresh } from '@features/library/domain/libraryView';

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

type TrackRowHandlers = Pick<
  TracksListProps,
  'onPlay' | 'onPress' | 'onMore' | 'onRetry' | 'isRetrying' | 'isPlaying'
>;

type TrackRowProps = {
  track: TrackResponse;
  handlers: TrackRowHandlers;
  selection: Selection | undefined;
};

function useTrackCallbacks(track: TrackResponse, handlers: TrackRowHandlers) {
  const { onPlay, onPress, onMore, onRetry } = handlers;
  const play = useCallback(() => onPlay(track), [onPlay, track]);
  const retry = useCallback(() => onRetry(track), [onRetry, track]);
  const press = useCallback(() => onPress(track), [onPress, track]);
  const more = useCallback((anchor: MenuAnchor) => onMore(track, anchor), [onMore, track]);
  return { play, retry, press, more };
}

function useSelectionCallbacks(track: TrackResponse, selection: Selection | undefined) {
  const begin = useCallback(() => selection?.begin(track.id), [selection, track.id]);
  const toggle = useCallback(() => selection?.toggle(track.id), [selection, track.id]);
  return { begin, toggle };
}

type TrackCallbacks = ReturnType<typeof useTrackCallbacks>;
type SelectionCallbacks = ReturnType<typeof useSelectionCallbacks>;

function rowCallbackProps(track: TrackResponse, calls: TrackCallbacks) {
  return {
    ...(track.acquisition_status === 'ready' ? { onPlay: calls.play } : null),
    ...(track.acquisition_status === 'failed' ? { onRetry: calls.retry } : null),
    onPress: calls.press,
    onMore: calls.more,
  };
}

function selectableProps(track: TrackResponse, selection: Selection, onToggle: () => void) {
  if (!selection.active) return undefined;
  return { selected: selection.has(track.id), onToggle };
}

function rowSelectionProps(
  track: TrackResponse,
  selection: Selection | undefined,
  calls: SelectionCallbacks,
) {
  if (selection == null) return null;
  const selectable = selectableProps(track, selection, calls.toggle);
  return { onLongPress: calls.begin, selectable };
}

function useRowSelectionProps(track: TrackResponse, selection: Selection | undefined) {
  const calls = useSelectionCallbacks(track, selection);
  return rowSelectionProps(track, selection, calls);
}

function useRowActionProps(track: TrackResponse, handlers: TrackRowHandlers) {
  return rowCallbackProps(track, useTrackCallbacks(track, handlers));
}

function rowStatusProps(track: TrackResponse, handlers: TrackRowHandlers) {
  return { retrying: handlers.isRetrying(track.id), isPlaying: handlers.isPlaying(track.id) };
}

function TrackRowImpl({ track, handlers, selection }: TrackRowProps): ReactElement {
  const actions = useRowActionProps(track, handlers);
  const selecting = useRowSelectionProps(track, selection);
  return (
    <LibraryRow track={track} {...actions} {...selecting} {...rowStatusProps(track, handlers)} />
  );
}

const TrackRow = memo(TrackRowImpl);

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
  const handlers = useMemo<TrackRowHandlers>(
    () => ({ onPlay, onPress, onMore, onRetry, isRetrying, isPlaying }),
    [onPlay, onPress, onMore, onRetry, isRetrying, isPlaying],
  );
  const renderItem = useCallback<ListRenderItem<TrackResponse>>(
    ({ item }) => <TrackRow track={item} handlers={handlers} selection={selection} />,
    [handlers, selection],
  );
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
      renderItem={renderItem}
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
