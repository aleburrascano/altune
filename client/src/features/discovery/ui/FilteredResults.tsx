import type { ReactElement } from 'react';
import { ScrollView, StyleSheet, View } from 'react-native';

import { Text, spacing } from '@shared/ui';

import { CorrectionBanner } from '@features/discovery/ui/CorrectionBanner';
import { DiscoverRow } from '@features/discovery/ui/DiscoverRow';
import { ResultsList, type ResultsCommonProps } from '@features/discovery/ui/ResultsList';
import { kindLabel, resultKey } from '@features/discovery/domain/resultDisplay';

import type { DiscoveryKind, DiscoveryResult } from '@shared/apiClient/discovery';
import type { UnreleasedSearchHit } from '@shared/apiClient/enrichment';
import { UnreleasedHitsSlot } from '@features/discovery/ports/UnreleasedHitsSlot';

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
        {common.correction != null ? (
          <CorrectionBanner
            correctedQuery={common.correction.corrected}
            originalQuery={common.correction.original}
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

interface UnreleasedAllHitsProps {
  hits: UnreleasedSearchHit[];
}

export function UnreleasedAllHits({ hits }: UnreleasedAllHitsProps): ReactElement {
  return (
    <ScrollView testID="discover-unreleased-all" contentContainerStyle={styles.allHits}>
      <UnreleasedHitsSlot hits={hits} />
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  allHits: { paddingTop: spacing.sm, paddingBottom: spacing.xl },
  filteredEmpty: {
    flex: 1,
    alignItems: 'center',
    justifyContent: 'center',
    padding: spacing['2xl'],
  },
});
