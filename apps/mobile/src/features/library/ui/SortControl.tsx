import { useState, type ReactElement } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';
import { ChevronDown } from 'lucide-react-native';

import { Text, spacing, useTheme } from '@shared/ui';
import { ActionSheet } from '@shared/ui/primitives/ActionSheet';

import type { SortKey } from './sort';

type SortOption = { key: SortKey; label: string };

/** Count + tappable sort label that opens an ActionSheet of the view's sort options. */
export function SortControl({
  count,
  noun,
  sortKey,
  options,
  onSortChange,
}: {
  count: number;
  noun: string;
  sortKey: SortKey;
  options: SortOption[];
  onSortChange: (key: SortKey) => void;
}): ReactElement {
  const theme = useTheme();
  const [menuVisible, setMenuVisible] = useState(false);
  const activeLabel = options.find((o) => o.key === sortKey)?.label ?? options[0]?.label ?? '';

  return (
    <View style={styles.row}>
      <Text variant="label" tone="tertiary">
        {count} {count === 1 ? noun : `${noun}s`}
      </Text>
      <Pressable
        testID="library-sort"
        onPress={() => setMenuVisible(true)}
        hitSlop={8}
        accessibilityRole="button"
        accessibilityLabel={`Sort by ${activeLabel}`}
        style={({ pressed }) => [styles.sort, pressed ? styles.pressed : null]}
      >
        <Text variant="label">{activeLabel}</Text>
        <ChevronDown size={14} color={theme.color.textPrimary} />
      </Pressable>
      <ActionSheet
        visible={menuVisible}
        title="Sort by"
        options={options.map((o) => ({
          label: o.label,
          testID: `library-sort-${o.key}`,
          onPress: () => onSortChange(o.key),
        }))}
        onClose={() => setMenuVisible(false)}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    paddingTop: spacing.md,
    paddingBottom: spacing.sm,
  },
  sort: { flexDirection: 'row', alignItems: 'center', gap: spacing.xs, minHeight: 32 },
  pressed: { opacity: 0.7 },
});
