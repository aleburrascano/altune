import type { ReactElement } from 'react';
import {
  FlatList,
  StyleSheet,
  type ListRenderItem,
  type StyleProp,
  type ViewStyle,
} from 'react-native';

import { LibraryEmptyMessage } from './LibraryEmptyMessage';
import { ListLoadingMoreFooter } from './ListLoadingMoreFooter';
import { listContent } from './listContentStyles';
import { GRID_GAP } from '../gridColumns';
import type { ListRefresh } from '../refresh';

type LibraryGridProps<TItem> = {
  testID: string;
  data: readonly TItem[];
  keyExtractor: (item: TItem) => string;
  columns: number;
  refresh: ListRefresh;
  renderItem: ListRenderItem<TItem>;
  columnWrapperStyle?: StyleProp<ViewStyle>;
  /** Omit to render nothing when the grid is empty. */
  emptyLabel?: string;
  /** Omit on a grid that holds every row it will ever hold. */
  onEndReached?: (() => void) | undefined;
  isFetchingNextPage?: boolean | undefined;
  nextPageFailed?: boolean | undefined;
  onRetryNextPage?: (() => void) | undefined;
};

export function LibraryGrid<TItem>({
  testID,
  data,
  keyExtractor,
  columns,
  refresh,
  renderItem,
  columnWrapperStyle = styles.gridRow,
  emptyLabel,
  onEndReached,
  isFetchingNextPage,
  nextPageFailed,
  onRetryNextPage,
}: LibraryGridProps<TItem>): ReactElement {
  return (
    <FlatList
      testID={testID}
      data={data}
      keyExtractor={keyExtractor}
      // FlatList cannot change numColumns in place, so a column count change remounts it.
      key={`cols-${columns}`}
      numColumns={columns}
      columnWrapperStyle={columnWrapperStyle}
      contentContainerStyle={data.length === 0 ? listContent.empty : listContent.padded}
      showsVerticalScrollIndicator={false}
      onRefresh={refresh.onRefresh}
      refreshing={refresh.refreshing}
      onEndReached={onEndReached}
      onEndReachedThreshold={0.5}
      ListFooterComponent={
        <ListLoadingMoreFooter
          loading={isFetchingNextPage === true}
          failed={nextPageFailed === true}
          onRetry={onRetryNextPage}
        />
      }
      ListEmptyComponent={emptyLabel != null ? <LibraryEmptyMessage label={emptyLabel} /> : null}
      renderItem={renderItem}
    />
  );
}

const styles = StyleSheet.create({
  gridRow: { gap: GRID_GAP },
});
