import type { ReactElement } from 'react';
import { StyleSheet, type ViewStyle } from 'react-native';
import { Chip, spacing } from '@shared/ui';
import { HorizontalRail } from '@shared/ui/primitives/HorizontalRail';
import type { LibraryChip } from '@features/library/domain/libraryView';

const CHIPS: ChipItem[] = [
  { key: 'playlists', label: 'Playlists' },
  { key: 'tracks', label: 'Tracks' },
  { key: 'albums', label: 'Albums' },
  { key: 'artists', label: 'Artists' },
];

type ChipViewProps = { action: string; label: string; selected: boolean; testID: string };

type RailStyleProps = { style: ViewStyle; contentContainerStyle: ViewStyle };

type ChipItem = { key: LibraryChip; label: string };

function chipKey(chip: ChipItem): string {
  return chip.key;
}

type ChipButtonProps = {
  chip: ChipItem;
  value: LibraryChip;
  onChange: (chip: LibraryChip) => void;
};

type LibraryChipsProps = {
  value: LibraryChip;
  onChange: (chip: LibraryChip) => void;
};

function chipViewProps(chip: ChipItem, selected: boolean): ChipViewProps {
  return {
    action: `library.chip.${chip.key}`,
    label: chip.label,
    selected,
    testID: `library-chip-${chip.key}`,
  };
}

function ChipButton({ chip, value, onChange }: ChipButtonProps): ReactElement {
  return <Chip {...chipViewProps(chip, value === chip.key)} onPress={() => onChange(chip.key)} />;
}

export function LibraryChips({ value, onChange }: LibraryChipsProps): ReactElement {
  return (
    <HorizontalRail
      {...railStyleProps()}
      data={CHIPS}
      keyExtractor={chipKey}
      renderItem={({ item }) => <ChipButton chip={item} value={value} onChange={onChange} />}
    />
  );
}

function railStyleProps(): RailStyleProps {
  return { style: styles.scroll, contentContainerStyle: styles.row };
}

const styles = StyleSheet.create({
  scroll: { flexGrow: 0 },
  row: { gap: spacing.sm, paddingVertical: spacing.xs },
});
