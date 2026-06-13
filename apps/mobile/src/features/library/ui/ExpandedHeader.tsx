import type { ReactElement } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';

import { Chip, Text, spacing, useTheme } from '@shared/ui';

import type { SortKey } from './sort';

export function ExpandedHeader({
  title,
  onBack,
  sortKey,
  onSortChange,
  sortOptions,
}: {
  title: string;
  onBack: () => void;
  sortKey: SortKey;
  onSortChange: (key: SortKey) => void;
  sortOptions: { key: SortKey; label: string }[];
}): ReactElement {
  const theme = useTheme();
  return (
    <View style={styles.header}>
      <View style={styles.titleRow}>
        <Pressable onPress={onBack} hitSlop={8} accessibilityRole="button" accessibilityLabel="Go back">
          <Text variant="label" style={{ color: theme.color.accent }}>
            ‹ Back
          </Text>
        </Pressable>
        <Text variant="title">{title}</Text>
        <View style={styles.spacer} />
      </View>
      <View style={styles.sortRow}>
        {sortOptions.map((opt) => (
          <Chip
            key={opt.key}
            label={opt.label}
            selected={sortKey === opt.key}
            onPress={() => onSortChange(opt.key)}
            testID={`sort-${opt.key}`}
          />
        ))}
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  header: { paddingBottom: spacing.sm },
  titleRow: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    paddingBottom: spacing.sm,
  },
  spacer: { width: 44 },
  sortRow: { flexDirection: 'row', gap: spacing.sm },
});
