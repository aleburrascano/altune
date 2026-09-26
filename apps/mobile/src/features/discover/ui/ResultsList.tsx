import type { ReactElement, ReactNode } from 'react';
import { FlatList, StyleSheet, type ListRenderItem, type ListRenderItemInfo } from 'react-native';

import { ActivityIndicator, Pressable, View } from 'react-native';

import { Text, spacing, useTheme, useWideWebLayout } from '@shared/ui';

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

type ResultsListProps<T> = {
  data: T[];
  keyExtractor: (item: T, index: number) => string;
  renderItem: ListRenderItem<T>;
  headerExtra?: ReactNode;
  common: ResultsCommonProps;
  pairFirstItemWithHeader?: boolean;
};

const noopSeparators = {
  highlight: () => undefined,
  unhighlight: () => undefined,
  updateProps: () => undefined,
};

function pairedHeaderInfo<T>(item: T): ListRenderItemInfo<T> {
  return { item, index: 0, separators: noopSeparators };
}

function PairedHeader<T>({ headerExtra, item, renderItem }: { headerExtra: ReactNode; item: T; renderItem: ListRenderItem<T> }): ReactElement {
  return (
    <View style={styles.pairedRow} testID="discover-top-pair">
      <View style={styles.pairedSide}>{headerExtra}</View>
      <View style={styles.pairedSide}>{renderItem(pairedHeaderInfo(item))}</View>
    </View>
  );
}

function CorrectionHeader({ common }: { common: ResultsCommonProps }): ReactElement | null {
  if (common.correction == null) return null;
  const { corrected, original } = common.correction;
  return <CorrectionBanner correctedQuery={corrected} originalQuery={original} onSearchOriginal={common.onSearchOriginal} />;
}

function ResultsHeader<T>({ common, paired, headerExtra, firstItem, renderItem }: { common: ResultsCommonProps; paired: boolean; headerExtra: ReactNode; firstItem: T; renderItem: ListRenderItem<T> }): ReactElement {
  return (
    <>
      <CorrectionHeader common={common} />
      {paired ? <PairedHeader headerExtra={headerExtra} item={firstItem} renderItem={renderItem} /> : headerExtra}
    </>
  );
}

function ResultsFlatList<T>({ items, header, keyExtractor, renderItem, common }: { items: T[]; header: ReactElement; keyExtractor: (item: T, index: number) => string; renderItem: ListRenderItem<T>; common: ResultsCommonProps }): ReactElement {
  return (
    <FlatList data={items} keyExtractor={keyExtractor} renderItem={renderItem} ListHeaderComponent={header}
      ListFooterComponent={<ResultsFooter common={common} />} style={styles.list} contentContainerStyle={styles.listContent}
      showsVerticalScrollIndicator={false} onRefresh={common.onRefresh} refreshing={common.isRefreshing}
      onViewableItemsChanged={common.impression.onViewableItemsChanged} viewabilityConfig={common.impression.viewabilityConfig}
      onEndReached={common.onEndReached} onEndReachedThreshold={0.5} />
  );
}

export function ResultsList<T>({ data: items, keyExtractor, renderItem, headerExtra, common, pairFirstItemWithHeader }: ResultsListProps<T>): ReactElement {
  const isWide = useWideWebLayout();
  const paired = isWide && pairFirstItemWithHeader === true && items.length > 0 && headerExtra != null;
  const listItems = paired ? items.slice(1) : items;
  const header = (
    <ResultsHeader common={common} paired={paired} headerExtra={headerExtra} firstItem={items[0] as T} renderItem={renderItem} />
  );
  return <ResultsFlatList items={listItems} header={header} keyExtractor={keyExtractor} renderItem={renderItem} common={common} />;
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
  pairedRow: { flexDirection: 'row', gap: spacing.xl, alignItems: 'flex-start' },
  pairedSide: { flex: 1 },
});

const retryFooterProps = {
  testID: 'discover-load-more-error',
  accessibilityRole: 'button',
  style: styles.footer,
} as const;
