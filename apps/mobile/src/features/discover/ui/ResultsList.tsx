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

type PairedHeaderProps<T> = { headerExtra: ReactNode; item: T; renderItem: ListRenderItem<T> };

function PairedHeader<T>({ headerExtra, item, renderItem }: PairedHeaderProps<T>): ReactElement {
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
  return (
    <CorrectionBanner
      correctedQuery={corrected}
      originalQuery={original}
      onSearchOriginal={common.onSearchOriginal}
    />
  );
}

type ResultsHeaderProps<T> = {
  common: ResultsCommonProps;
  paired: boolean;
  headerExtra: ReactNode;
  firstItem: T;
  renderItem: ListRenderItem<T>;
};

function pairedOrExtra<T>(props: ResultsHeaderProps<T>): ReactNode {
  const { paired, headerExtra, firstItem, renderItem } = props;
  if (!paired) return headerExtra;
  return <PairedHeader headerExtra={headerExtra} item={firstItem} renderItem={renderItem} />;
}

function ResultsHeader<T>(props: ResultsHeaderProps<T>): ReactElement {
  return (
    <>
      <CorrectionHeader common={props.common} />
      {pairedOrExtra(props)}
    </>
  );
}

type ResultsFlatListProps<T> = {
  items: T[];
  header: ReactElement;
  keyExtractor: (item: T, index: number) => string;
  renderItem: ListRenderItem<T>;
  common: ResultsCommonProps;
};

function flatListImpressionProps(common: ResultsCommonProps) {
  return {
    onViewableItemsChanged: common.impression.onViewableItemsChanged,
    viewabilityConfig: common.impression.viewabilityConfig,
  };
}

function flatListChromeProps(header: ReactElement, common: ResultsCommonProps) {
  return {
    ListHeaderComponent: header,
    ListFooterComponent: <ResultsFooter common={common} />,
    style: styles.list,
    contentContainerStyle: styles.listContent,
    showsVerticalScrollIndicator: false,
  };
}

function flatListRefreshProps(common: ResultsCommonProps) {
  return {
    onRefresh: common.onRefresh,
    refreshing: common.isRefreshing,
    ...flatListImpressionProps(common),
    onEndReached: common.onEndReached,
    onEndReachedThreshold: 0.5,
  };
}

function flatListProps<T>(props: ResultsFlatListProps<T>) {
  const { header, keyExtractor, renderItem, common } = props;
  return {
    keyExtractor,
    renderItem,
    ...flatListChromeProps(header, common),
    ...flatListRefreshProps(common),
  };
}

function ResultsFlatList<T>(props: ResultsFlatListProps<T>): ReactElement {
  return <FlatList data={props.items} {...flatListProps(props)} />;
}

function usePairedFirstItem<T>(
  items: T[],
  pairFirstItemWithHeader: boolean | undefined,
  headerExtra: ReactNode,
) {
  const isWide = useWideWebLayout();
  const paired =
    isWide && pairFirstItemWithHeader === true && items.length > 0 && headerExtra != null;
  return { paired, listItems: paired ? items.slice(1) : items };
}

function resultsHeaderProps<T>(props: ResultsListProps<T>, paired: boolean): ResultsHeaderProps<T> {
  const { common, headerExtra, renderItem } = props;
  return { common, paired, headerExtra, firstItem: props.data[0] as T, renderItem };
}

function resultsFlatListProps<T>(props: ResultsListProps<T>, listItems: T[], header: ReactElement) {
  const { keyExtractor, renderItem, common } = props;
  return { items: listItems, header, keyExtractor, renderItem, common };
}

export function ResultsList<T>(props: ResultsListProps<T>): ReactElement {
  const { data: items, pairFirstItemWithHeader, headerExtra } = props;
  const { paired, listItems } = usePairedFirstItem(items, pairFirstItemWithHeader, headerExtra);
  const header = <ResultsHeader {...resultsHeaderProps(props, paired)} />;
  return <ResultsFlatList {...resultsFlatListProps(props, listItems, header)} />;
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
