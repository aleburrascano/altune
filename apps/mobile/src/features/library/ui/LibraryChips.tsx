import type { ReactElement } from 'react';
import { ScrollView, StyleSheet } from 'react-native';

import { Chip, spacing } from '@shared/ui';

export type LibraryChip = 'playlists' | 'songs' | 'albums' | 'artists';

const CHIPS: { key: LibraryChip; label: string }[] = [
  { key: 'playlists', label: 'Playlists' },
  { key: 'songs', label: 'Songs' },
  { key: 'albums', label: 'Albums' },
  { key: 'artists', label: 'Artists' },
];

export function LibraryChips({
  value,
  onChange,
}: {
  value: LibraryChip;
  onChange: (chip: LibraryChip) => void;
}): ReactElement {
  return (
    <ScrollView
      horizontal
      showsHorizontalScrollIndicator={false}
      contentContainerStyle={styles.row}
    >
      {CHIPS.map((chip) => (
        <Chip
          key={chip.key}
          label={chip.label}
          selected={value === chip.key}
          onPress={() => onChange(chip.key)}
          testID={`library-chip-${chip.key}`}
        />
      ))}
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  row: { gap: spacing.sm, paddingVertical: spacing.xs },
});
