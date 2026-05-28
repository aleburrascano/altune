import type { ReactNode } from 'react';
import { Pressable, View } from 'react-native';

import { radius, spacing } from '../theme/tokens';
import { useTheme } from '../theme/useTheme';
import { Text } from './Text';

export type ChipProps = {
  label: string;
  /** When provided, the chip becomes a button (recent-search rows, filters). */
  onPress?: () => void;
  selected?: boolean;
  icon?: ReactNode;
  testID?: string;
};

export function Chip({ label, onPress, selected = false, icon, testID }: ChipProps) {
  const theme = useTheme();
  const body = (
    <View
      style={{
        flexDirection: 'row',
        alignItems: 'center',
        gap: spacing.xs,
        paddingVertical: spacing.sm,
        paddingHorizontal: spacing.md,
        borderRadius: radius.full,
        backgroundColor: selected ? theme.color.accentTint : theme.color.surface2,
      }}
    >
      {icon != null ? icon : null}
      <Text variant="label" tone={selected ? 'accent' : 'secondary'}>
        {label}
      </Text>
    </View>
  );

  if (onPress) {
    return (
      <Pressable
        testID={testID}
        onPress={onPress}
        accessibilityRole="button"
        accessibilityLabel={label}
        hitSlop={8}
        style={({ pressed }) => (pressed ? { opacity: 0.7 } : null)}
      >
        {body}
      </Pressable>
    );
  }
  return <View testID={testID}>{body}</View>;
}
