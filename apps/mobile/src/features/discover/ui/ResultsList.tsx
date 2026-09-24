import type { ReactElement, ReactNode } from 'react';
import { FlatList, StyleSheet, type ListRenderItem } from 'react-native';

import { ActivityIndicator, Pressable, View } from 'react-native';

import { Text, spacing, useTheme } from '@shared/ui';

import { CorrectionBanner } from './CorrectionBanner';
import type { DiscoveryResult } from '@shared/api-client/discovery';
import type { ImpressionHandlers } from '../hooks/useImpressionLogger';
import type { SearchCorrection } from '../state';

export type ResultsCommonProps = {
  onResultTap: (result: DiscoveryResult, position: number) => void;
  impression: ImpressionHandlers;
  onRefresh: () => void;
  isRefreshing: boolean;
  onEndReached: () => void;
  isFetchingNextPage: boolean;
  nextPageFailed?: boolean | undefined;
  onRetryNextPage?: (() => void) | undefined;
  correction: SearchCorrection | null;
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
  const header = (
    <>
      {common.correction != null ? (
        <CorrectionBanner
          correctedQuery={common.correction.corrected}
          originalQuery={common.correction.original}
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
        <ResultsFooter common={common} />
      }
    />
  );
}

function ResultsFooter({ common }: { common: ResultsCommonProps }): ReactElement | null {
  if (common.nextPageFailed === true) return <RetryFooter onRetry={common.onRetryNextPage} />;
  if (!common.isFetchingNextPage) return null;
  return <LoadingFooter />;
}

function RetryFooter({ onRetry }: { onRetry: (() => void) | undefined }): ReactElement {
  return (
    <Pressable {...retryFooterProps} onPress={onRetry}>
      <RetryLabel />
    </Pressable>
  );
}

function RetryLabel(): ReactElement {
  return (
    <Text variant="label" tone="secondary">
      Couldn't load more. Tap to retry.
    </Text>
  );
}

function LoadingFooter(): ReactElement {
  const theme = useTheme();
  return (
    <View testID="discover-loading-more" style={styles.footer}>
      <ActivityIndicator size="small" color={theme.color.accent} />
    </View>
  );
}

const styles = StyleSheet.create({
  list: { flex: 1 },
  listContent: { paddingTop: spacing.sm, paddingBottom: spacing.xl, flexGrow: 1 },
  footer: { paddingVertical: spacing.xl, alignItems: 'center' },
});

const retryFooterProps = {
  testID: 'discover-load-more-error',
  accessibilityRole: 'button',
  style: styles.footer,
} as const;
