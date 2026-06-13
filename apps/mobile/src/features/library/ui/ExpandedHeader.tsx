import type { ReactElement } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';

import { Chip, Text, spacing, useTheme } from '@shared/ui';

import type { SortKey } from './sort';

export function ExpandedHeader({
  title,
  onCollapse,
  sortKey,
  onSortChange,
  sortOptions,
}: {
  title: string;
  onCollapse: () => void;
  sortKey: SortKey;
  onSortChange: (key: SortKey) => void;
  sortOptions: { key: SortKey; label: string }[];
}): ReactElement {
  const theme = useTheme();
  return (
    <View style={styles.expandedHeader}>
      <View style={styles.expandedTitleRow}>
        <Text variant="title">{title}</Text>
        <Pressable onPress={onCollapse} hitSlop={8}>
          <Text variant="label" style={{ color: theme.color.accent }}>
            Collapse ↑
          </Text>
        </Pressable>
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
  expandedHeader: { paddingBottom: spacing.sm },
  expandedTitleRow: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    paddingBottom: spacing.sm,
  },
  sortRow: { flexDirection: 'row', gap: spacing.sm },
});
