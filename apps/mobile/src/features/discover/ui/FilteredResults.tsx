import type { ReactElement } from 'react';
import { FlatList, StyleSheet, View } from 'react-native';

import { Text, spacing } from '@shared/ui';

import { DiscoverRow } from './DiscoverRow';

import type {
  DiscoveryKind,
  DiscoveryResult,
} from '../../../shared/api-client/discovery';

export function FilteredResults({
  kind,
  results,
  onResultTap,
}: {
  kind: DiscoveryKind;
  results: DiscoveryResult[];
  onResultTap: (result: DiscoveryResult, position: number) => void;
}): ReactElement {
  const items = results.filter((r) => r.kind === kind);
  const kindLabel = kind === 'track' ? 'songs' : kind === 'album' ? 'albums' : 'artists';

  if (items.length === 0) {
    return (
      <View testID="discover-filtered-empty" style={styles.filteredEmpty}>
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
      renderItem={({ item, index }) => (
        <DiscoverRow result={item} position={index} onPress={onResultTap} />
      )}
      contentContainerStyle={styles.listContent}
      showsVerticalScrollIndicator={false}
    />
  );
}

const styles = StyleSheet.create({
  listContent: { paddingTop: spacing.sm, paddingBottom: spacing.xl },
  filteredEmpty: { flex: 1, alignItems: 'center', justifyContent: 'center', padding: spacing['2xl'] },
});
