import type { ReactElement } from 'react';
import { FlatList, StyleSheet, View } from 'react-native';

import { Text, spacing } from '@shared/ui';

import { CorrectionBanner } from './CorrectionBanner';
import { DiscoverRow } from './DiscoverRow';

import type {
  DiscoveryKind,
  DiscoveryResult,
} from '../../../shared/api-client/discovery';
import type { ImpressionHandlers } from '../hooks/useImpressionLogger';

export function FilteredResults({
  kind,
  results,
  onResultTap,
  impression,
  onRefresh,
  isRefreshing,
  correctedQuery,
  originalQuery,
  onSearchOriginal,
}: {
  kind: DiscoveryKind;
  results: DiscoveryResult[];
  onResultTap: (result: DiscoveryResult, position: number) => void;
  impression: ImpressionHandlers;
  onRefresh: () => void;
  isRefreshing: boolean;
  correctedQuery?: string | undefined;
  originalQuery?: string | undefined;
  onSearchOriginal: () => void;
}): ReactElement {
  const items = results.filter((r) => r.kind === kind);
  const kindLabel = kind === 'track' ? 'songs' : kind === 'album' ? 'albums' : 'artists';

  const header = correctedQuery && originalQuery ? (
    <CorrectionBanner
      correctedQuery={correctedQuery}
      originalQuery={originalQuery}
      onSearchOriginal={onSearchOriginal}
    />
  ) : null;

  if (items.length === 0) {
    return (
      <View testID="discover-filtered-empty" style={styles.filteredEmpty}>
        {header}
        <Text variant="body" tone="tertiary">
          No {kindLabel} found.
        </Text>
      </View>
    );
  }

  return (
    <FlatList
      data={items}
      keyExtractor={(r) => `${r.kind}-${r.sources[0]?.provider ?? 'x'}-${r.sources[0]?.external_id ?? r.title}`}
      ListHeaderComponent={header}
      renderItem={({ item, index }) => (
        <DiscoverRow result={item} position={index} onPress={onResultTap} />
      )}
      contentContainerStyle={styles.listContent}
      showsVerticalScrollIndicator={false}
      onRefresh={onRefresh}
      refreshing={isRefreshing}
      onViewableItemsChanged={impression.onViewableItemsChanged}
      viewabilityConfig={impression.viewabilityConfig}
    />
  );
}

const styles = StyleSheet.create({
  listContent: { paddingTop: spacing.sm, paddingBottom: spacing.xl, flexGrow: 1 },
  filteredEmpty: { flex: 1, alignItems: 'center', justifyContent: 'center', padding: spacing['2xl'] },
});
