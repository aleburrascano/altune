import type { ReactElement } from 'react';
import { ScrollView, StyleSheet } from 'react-native';

import { Chip, spacing } from '@shared/ui';

export type LibraryChip = 'playlists' | 'tracks' | 'albums' | 'artists';

const CHIPS: { key: LibraryChip; label: string }[] = [
  { key: 'playlists', label: 'Playlists' },
  { key: 'tracks', label: 'Tracks' },
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
      style={styles.scroll}
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
  scroll: { flexGrow: 0 },
  row: { gap: spacing.sm, paddingVertical: spacing.xs },
});
