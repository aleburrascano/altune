import type { ReactElement } from 'react';
import { StyleSheet } from 'react-native';
import { Chip, spacing } from '@shared/ui';
import { HorizontalRail } from '@shared/ui/primitives/HorizontalRail';
import type { LibraryChip } from '@features/library/domain/libraryView';

const CHIPS: ChipItem[] = [
  { key: 'playlists', label: 'Playlists' },
  { key: 'tracks', label: 'Tracks' },
  { key: 'albums', label: 'Albums' },
  { key: 'artists', label: 'Artists' },
];

type ChipItem = { key: LibraryChip; label: string };

function chipKey(chip: ChipItem): string {
  return chip.key;
}

function ChipButton({
  chip,
  value,
  onChange,
}: {
  chip: ChipItem;
  value: LibraryChip;
  onChange: (chip: LibraryChip) => void;
}): ReactElement {
  return (
    <Chip
      action={`library.chip.${chip.key}`}
      label={chip.label}
      selected={value === chip.key}
      onPress={() => onChange(chip.key)}
      testID={`library-chip-${chip.key}`}
    />
  );
}

export function LibraryChips({
  value,
  onChange,
}: {
  value: LibraryChip;
  onChange: (chip: LibraryChip) => void;
}): ReactElement {
  return (
    <HorizontalRail
      data={CHIPS}
      keyExtractor={chipKey}
      renderItem={({ item }) => <ChipButton chip={item} value={value} onChange={onChange} />}
      style={styles.scroll}
      contentContainerStyle={styles.row}
    />
  );
}

const styles = StyleSheet.create({
  scroll: { flexGrow: 0 },
  row: { gap: spacing.sm, paddingVertical: spacing.xs },
});
