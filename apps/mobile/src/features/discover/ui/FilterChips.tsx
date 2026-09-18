import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { Chip, spacing } from '@shared/ui';

import { kindLabel } from '../kindLabel';
import type { ResultsFilter } from '../hooks/useResultsFilter';

const FILTER_CHIPS: readonly { filter: ResultsFilter; label: string; testID: string }[] = [
  { filter: 'all', label: 'All', testID: 'discover-filter-all' },
  { filter: 'album', label: kindLabel('album', { plural: true }), testID: 'discover-filter-album' },
  { filter: 'track', label: kindLabel('track', { plural: true }), testID: 'discover-filter-track' },
  {
    filter: 'artist',
    label: kindLabel('artist', { plural: true }),
    testID: 'discover-filter-artist',
  },
];

export function FilterChips({
  active,
  onSelect,
}: {
  active: ResultsFilter;
  onSelect: (filter: ResultsFilter) => void;
}): ReactElement {
  return (
    <View style={styles.chipRow}>
      {FILTER_CHIPS.map(({ filter, label, testID }) => (
        <Chip
          key={filter}
          testID={testID}
          label={label}
          selected={active === filter}
          onPress={() => onSelect(filter)}
        />
      ))}
    </View>
  );
}

const styles = StyleSheet.create({
  chipRow: {
    flexDirection: 'row',
    gap: spacing.sm,
    paddingBottom: spacing.md,
    flexWrap: 'wrap',
  },
});
