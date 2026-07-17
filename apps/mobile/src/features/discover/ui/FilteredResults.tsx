import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { Text, spacing } from '@shared/ui';

import { CorrectionBanner } from './CorrectionBanner';
import { DiscoverRow } from './DiscoverRow';
import { ResultsList, type ResultsCommonProps } from './ResultsList';
import { kindLabel, resultKey } from '../state';

import type { DiscoveryKind, DiscoveryResult } from '@shared/api-client/discovery';

export function FilteredResults({
  kind,
  results,
  common,
}: {
  kind: DiscoveryKind;
  results: DiscoveryResult[];
  common: ResultsCommonProps;
}): ReactElement {
  const items = results.filter((r) => r.kind === kind);

  if (items.length === 0) {
    return (
      <View testID="discover-filtered-empty" style={styles.filteredEmpty}>
        {common.correctedQuery && common.originalQuery ? (
          <CorrectionBanner
            correctedQuery={common.correctedQuery}
            originalQuery={common.originalQuery}
            onSearchOriginal={common.onSearchOriginal}
          />
        ) : null}
        <Text variant="body" tone="tertiary">
          No {kindLabel(kind, { plural: true }).toLowerCase()} found.
        </Text>
      </View>
    );
  }

  return (
    <ResultsList
      data={items}
      keyExtractor={resultKey}
      common={common}
      renderItem={({ item, index }) => (
        <DiscoverRow result={item} position={index} onPress={common.onResultTap} />
      )}
    />
  );
}

const styles = StyleSheet.create({
  filteredEmpty: { flex: 1, alignItems: 'center', justifyContent: 'center', padding: spacing['2xl'] },
});
