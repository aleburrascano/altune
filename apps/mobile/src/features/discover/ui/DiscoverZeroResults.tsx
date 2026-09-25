import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { Text, spacing } from '@shared/ui';

import { FilterChips } from './FilterChips';
import { IncompleteResultsBanner } from './IncompleteResultsBanner';
import type { ResultsFilter } from '../hooks/useResultsFilter';

interface ZeroResultsProps {
  filter: ResultsFilter;
  onFilterChange: (filter: ResultsFilter) => void;
  resultsIncomplete?: boolean | undefined;
}

function NoMatches(): ReactElement {
  return (
    <View style={styles.center}>
      <Text variant="title">No matches</Text>
      <Text variant="label" tone="secondary" style={styles.sub}>
        Check spelling or try fewer words.
      </Text>
    </View>
  );
}

export function DiscoverZeroResults(props: ZeroResultsProps): ReactElement {
  return (
    <View testID="discover-zero-results" style={styles.root}>
      <FilterChips active={props.filter} onSelect={props.onFilterChange} />
      <IncompleteResultsBanner visible={props.resultsIncomplete} />
      <NoMatches />
    </View>
  );
}

const styles = StyleSheet.create({
  root: { flex: 1 },
  center: { flex: 1, alignItems: 'center', justifyContent: 'center', padding: spacing['2xl'] },
  sub: { marginTop: spacing.xs, marginBottom: spacing.lg },
});
