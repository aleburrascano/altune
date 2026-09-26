import type { ReactElement } from 'react';
import {
  FlatList,
  StyleSheet,
  type LayoutChangeEvent,
  type ListRenderItem,
  type StyleProp,
  type ViewStyle,
} from 'react-native';

import { useWideWebLayout } from '@shared/ui';

import { LibraryEmptyMessage } from './LibraryEmptyMessage';
import { ListLoadingMoreFooter } from './ListLoadingMoreFooter';
import { listContent } from './listContentStyles';
import { GRID_GAP } from '../gridColumns';
import type { ListPaging, ListRefresh } from '../refresh';

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
  paging?: ListPaging | undefined;
  onLayout?: (event: LayoutChangeEvent) => void;
};

function gridRowStyle(isWide: boolean, columnWrapperStyle: StyleProp<ViewStyle> | undefined) {
  if (columnWrapperStyle != null) return columnWrapperStyle;
  return isWide ? styles.gridRowWide : styles.gridRow;
}

export function LibraryGrid<TItem>({
  testID,
  data,
  keyExtractor,
  columns,
  refresh,
  renderItem,
  columnWrapperStyle,
  emptyLabel,
  paging,
  onLayout,
}: LibraryGridProps<TItem>): ReactElement {
  const isWide = useWideWebLayout();
  return (
    <FlatList
      testID={testID}
      data={data}
      keyExtractor={keyExtractor}
      // FlatList cannot change numColumns in place, so a column count change remounts it.
      key={`cols-${columns}`}
      numColumns={columns}
      onLayout={onLayout}
      columnWrapperStyle={gridRowStyle(isWide, columnWrapperStyle)}
      contentContainerStyle={data.length === 0 ? listContent.empty : listContent.padded}
      showsVerticalScrollIndicator={false}
      onRefresh={refresh.onRefresh}
      refreshing={refresh.refreshing}
      onEndReached={paging?.onEndReached}
      onEndReachedThreshold={0.5}
      ListFooterComponent={
        <ListLoadingMoreFooter
          loading={paging?.isFetchingNextPage === true}
          failed={paging?.nextPageFailed === true}
          onRetry={paging?.onRetryNextPage}
        />
      }
      ListEmptyComponent={emptyLabel != null ? <LibraryEmptyMessage label={emptyLabel} /> : null}
      renderItem={renderItem}
    />
  );
}

const styles = StyleSheet.create({
  gridRow: { gap: GRID_GAP },
  gridRowWide: { gap: GRID_GAP * 2 },
});
