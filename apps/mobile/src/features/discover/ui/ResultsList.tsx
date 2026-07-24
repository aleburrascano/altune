import type { ReactElement, ReactNode } from 'react';
import { FlatList, StyleSheet, type ListRenderItem } from 'react-native';

import { ActivityIndicator, View } from 'react-native';

import { spacing, useTheme } from '@shared/ui';

import { CorrectionBanner } from './CorrectionBanner';
import type { DiscoveryResult } from '@shared/api-client/discovery';
import type { ImpressionHandlers } from '../hooks/useImpressionLogger';

export type ResultsCommonProps = {
  onResultTap: (result: DiscoveryResult, position: number) => void;
  impression: ImpressionHandlers;
  onRefresh: () => void;
  isRefreshing: boolean;
  onEndReached: () => void;
  isFetchingNextPage: boolean;
  correctedQuery?: string | undefined;
  originalQuery?: string | undefined;
  onSearchOriginal: () => void;
};

export function ResultsList<T>({
  data,
  keyExtractor,
  renderItem,
  headerExtra,
  common,
}: {
  data: T[];
  keyExtractor: (item: T, index: number) => string;
  renderItem: ListRenderItem<T>;
  headerExtra?: ReactNode;
  common: ResultsCommonProps;
}): ReactElement {
  const theme = useTheme();
  const header = (
    <>
      {common.correctedQuery && common.originalQuery ? (
        <CorrectionBanner
          correctedQuery={common.correctedQuery}
          originalQuery={common.originalQuery}
          onSearchOriginal={common.onSearchOriginal}
        />
      ) : null}
      {headerExtra}
    </>
  );

  return (
    <FlatList
      data={data}
      keyExtractor={keyExtractor}
      ListHeaderComponent={header}
      renderItem={renderItem}
      style={styles.list}
      contentContainerStyle={styles.listContent}
      showsVerticalScrollIndicator={false}
      onRefresh={common.onRefresh}
      refreshing={common.isRefreshing}
      onViewableItemsChanged={common.impression.onViewableItemsChanged}
      viewabilityConfig={common.impression.viewabilityConfig}
      onEndReached={common.onEndReached}
      onEndReachedThreshold={0.5}
      ListFooterComponent={
        common.isFetchingNextPage ? (
          <View testID="discover-loading-more" style={styles.footer}>
            <ActivityIndicator size="small" color={theme.color.accent} />
          </View>
        ) : null
      }
    />
  );
}

const styles = StyleSheet.create({
  list: { flex: 1 },
  listContent: { paddingTop: spacing.sm, paddingBottom: spacing.xl, flexGrow: 1 },
  footer: { paddingVertical: spacing.xl, alignItems: 'center' },
});
