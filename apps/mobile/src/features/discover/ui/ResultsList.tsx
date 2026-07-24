/**
 * ResultsList — the shared results-FlatList shell for both results surfaces
 * (blended "All" view and single-kind filtered view).
 *
 * Owns the chrome the two surfaces used to hand-roll separately (and let
 * drift): correction-banner header, pull-to-refresh, and the impression
 * viewability wiring. The surfaces supply only what genuinely varies —
 * data, key, renderItem, and an optional extra header (the Top Result card).
 */

import type { ReactElement, ReactNode } from 'react';
import { FlatList, StyleSheet, type ListRenderItem } from 'react-native';

import { ActivityIndicator, View } from 'react-native';

import { spacing, useTheme } from '@shared/ui';

import { CorrectionBanner } from './CorrectionBanner';
import type { DiscoveryResult } from '@shared/api-client/discovery';
import type { ImpressionHandlers } from '../hooks/useImpressionLogger';

/**
 * The props both results surfaces share, built once in DiscoverBody and passed
 * as a single object — a new results-surface concern extends this type instead
 * of threading one more prop through every layer.
 */
export type ResultsCommonProps = {
  onResultTap: (result: DiscoveryResult, position: number) => void;
  impression: ImpressionHandlers;
  onRefresh: () => void;
  isRefreshing: boolean;
  /** Infinite scroll over the ranked slate. */
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
      // Half a screen of runway: enough to hide the fetch, not so much that it
      // pages ahead of anything the user has actually looked at.
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
