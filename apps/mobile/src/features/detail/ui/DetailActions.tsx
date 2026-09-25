import type { ComponentType, ReactElement, ReactNode } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';

import { tapFeedback } from '@shared/ui/haptics';
import { Text } from '@shared/ui/primitives/Text';
import { minInteractiveHeight, radius, spacing, useTheme } from '@shared/ui/theme';

type Glyph = ComponentType<{ size?: number; color?: string; fill?: string }>;

export type PrimaryAction = {
  label: string;
  icon: Glyph;
  onPress: () => void;
  disabled?: boolean;
  testID?: string;
  accessibilityLabel: string;
};

export function DetailActions({
  primary,
  secondary,
}: {
  primary: PrimaryAction;
  secondary?: ReactNode;
}): ReactElement {
  return (
    <View style={styles.row}>
      <PrimaryPill {...primary} />
      {secondary}
    </View>
  );
}

function PrimaryPill({
  label,
  icon: Icon,
  onPress,
  disabled = false,
  testID,
  accessibilityLabel,
}: PrimaryAction): ReactElement {
  const theme = useTheme();

  return (
    <Pressable
      testID={testID}
      onPress={() => {
        if (disabled) {
          return;
        }
        tapFeedback();
        onPress();
      }}
      disabled={disabled}
      accessibilityRole="button"
      accessibilityLabel={accessibilityLabel}
      accessibilityState={{ disabled, busy: false }}
      style={({ pressed }) => [
        styles.pill,
        {
          backgroundColor: disabled
            ? theme.color.surface2
            : pressed
              ? theme.color.accentPressed
              : theme.color.accent,
        },
      ]}
    >
      <Icon
        size={18}
        color={disabled ? theme.color.textTertiary : theme.color.onAccent}
        fill={disabled ? theme.color.textTertiary : theme.color.onAccent}
      />
      <Text variant="bodyStrong" tone={disabled ? 'tertiary' : 'onAccent'}>
        {label}
      </Text>
    </Pressable>
  );
}

export function SecondaryAction({
  icon: Icon,
  onPress,
  accessibilityLabel,
  testID,
}: {
  icon: Glyph;
  onPress: () => void;
  accessibilityLabel: string;
  testID?: string;
}): ReactElement {
  const theme = useTheme();
  return (
    <Pressable
      testID={testID}
      onPress={onPress}
      accessibilityRole="button"
      accessibilityLabel={accessibilityLabel}
      style={({ pressed }) => [
        styles.circle,
        {
          borderColor: theme.color.border,
          backgroundColor: pressed ? theme.color.surface2 : theme.color.surface1,
        },
      ]}
    >
      <Icon size={20} color={theme.color.textPrimary} />
    </Pressable>
  );
}

const CIRCLE = 48;

const styles = StyleSheet.create({
  row: { flexDirection: 'row', alignItems: 'center', gap: spacing.sm, marginTop: spacing.lg },
  pill: {
    flex: 1,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: spacing.sm,
    minHeight: minInteractiveHeight,
    paddingHorizontal: spacing.lg,
    borderRadius: radius.full,
  },
  circle: {
    width: CIRCLE,
    height: CIRCLE,
    borderRadius: radius.full,
    borderWidth: StyleSheet.hairlineWidth,
    alignItems: 'center',
    justifyContent: 'center',
    flexShrink: 0,
  },
});
