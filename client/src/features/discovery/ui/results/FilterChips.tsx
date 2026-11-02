import type { ReactElement } from 'react';
import { StyleSheet, View } from 'react-native';

import { Chip, spacing } from '@shared/ui';

import { kindLabel } from '@features/discovery/domain/resultDisplay';
import type { ResultsFilter } from '@features/discovery/hooks/results/useResultsFilter';

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

const UNRELEASED_CHIP = {
  filter: 'unreleased',
  label: 'Unreleased',
  testID: 'discover-filter-unreleased',
} as const;

export function FilterChips({
  active,
  onSelect,
  showUnreleased = false,
}: {
  active: ResultsFilter;
  onSelect: (filter: ResultsFilter) => void;
  showUnreleased?: boolean;
}): ReactElement {
  const chips = showUnreleased ? [...FILTER_CHIPS, UNRELEASED_CHIP] : FILTER_CHIPS;
  return (
    <View style={styles.chipRow}>
      {chips.map(({ filter, label, testID }) => (
        <Chip
          key={filter}
          action={`discover.filter.${filter}`}
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
