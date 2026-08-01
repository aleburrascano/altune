import type { ReactElement } from 'react';
import { Pressable, ScrollView, StyleSheet, View } from 'react-native';
import type { LucideIcon } from 'lucide-react-native';

import { Text, minInteractiveHeight, radius, spacing, useTheme } from '@shared/ui';

export type SelectionAction = {
  key: string;
  label: string;
  icon: LucideIcon;
  onPress: () => void;
  tone?: 'danger';
  disabled?: boolean;
};

type SelectionBarProps = {
  count: number;
  allSelected: boolean;
  onSelectAll: () => void;
  onCancel: () => void;
  actions: SelectionAction[];
};

export function SelectionBar({
  count,
  allSelected,
  onSelectAll,
  onCancel,
  actions,
}: SelectionBarProps): ReactElement {
  const theme = useTheme();

  return (
    <View
      testID="selection-bar"
      style={[
        styles.bar,
        { backgroundColor: theme.color.surface1, borderTopColor: theme.color.border },
      ]}
    >
      <View style={styles.header}>
        <Text variant="bodyStrong" testID="selection-count">
          {count} selected
        </Text>
        <View style={styles.headerActions}>
          <Pressable
            testID="selection-select-all"
            onPress={onSelectAll}
            hitSlop={8}
            accessibilityRole="button"
            accessibilityLabel={allSelected ? 'Deselect all tracks' : 'Select all tracks'}
          >
            <Text variant="label" style={{ color: theme.color.accent }}>
              {allSelected ? 'Deselect all' : 'Select all'}
            </Text>
          </Pressable>
          <Pressable
            testID="selection-cancel"
            onPress={onCancel}
            hitSlop={8}
            accessibilityRole="button"
            accessibilityLabel="Exit selection"
          >
            <Text variant="label" tone="secondary">
              Cancel
            </Text>
          </Pressable>
        </View>
      </View>

      <ScrollView
        horizontal
        showsHorizontalScrollIndicator={false}
        contentContainerStyle={styles.actions}
      >
        {actions.map((action) => {
          const Icon = action.icon;
          const color = action.disabled
            ? theme.color.textTertiary
            : action.tone === 'danger'
              ? theme.color.danger
              : theme.color.textPrimary;
          return (
            <Pressable
              key={action.key}
              testID={`selection-action-${action.key}`}
              onPress={action.onPress}
              disabled={action.disabled}
              accessibilityRole="button"
              accessibilityLabel={action.label}
              accessibilityState={{ disabled: action.disabled === true }}
              style={({ pressed }) => [
                styles.action,
                { backgroundColor: theme.color.surface2 },
                pressed && !action.disabled ? styles.pressed : null,
              ]}
            >
              <Icon size={16} color={color} />
              <Text variant="label" style={{ color }}>
                {action.label}
              </Text>
            </Pressable>
          );
        })}
      </ScrollView>
    </View>
  );
}

const styles = StyleSheet.create({
  bar: {
    borderTopWidth: StyleSheet.hairlineWidth,
    paddingTop: spacing.md,
    paddingBottom: spacing.md,
    gap: spacing.md,
  },
  header: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    paddingHorizontal: spacing.lg,
  },
  headerActions: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.lg,
  },
  actions: {
    flexDirection: 'row',
    gap: spacing.sm,
    paddingHorizontal: spacing.lg,
  },
  action: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.xs,
    minHeight: minInteractiveHeight,
    paddingHorizontal: spacing.md,
    borderRadius: radius.full,
  },
  pressed: { opacity: 0.6 },
});
