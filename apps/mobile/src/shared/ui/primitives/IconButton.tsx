import type { ComponentType } from 'react';
import { Pressable } from 'react-native';

import { useTheme } from '../theme/useTheme';

export type IconButtonProps = {
  /** A lucide-react-native icon (or any component taking size/color). */
  icon: ComponentType<{ size?: number; color?: string }>;
  onPress: () => void;
  accessibilityLabel: string;
  size?: number;
  color?: string;
  /** Blocks presses and announces the disabled state. Pass a dimmed `color` too — this doesn't tint. */
  disabled?: boolean;
  testID?: string;
};

/** ≥44pt tappable icon with a required a11y label. */
export function IconButton({
  icon: Icon,
  onPress,
  accessibilityLabel,
  size = 24,
  color,
  disabled = false,
  testID,
}: IconButtonProps) {
  const theme = useTheme();
  return (
    <Pressable
      testID={testID}
      onPress={onPress}
      disabled={disabled}
      accessibilityRole="button"
      accessibilityLabel={accessibilityLabel}
      accessibilityState={{ disabled }}
      hitSlop={12}
      style={({ pressed }) => [
        { minWidth: 44, minHeight: 44, alignItems: 'center', justifyContent: 'center' },
        pressed ? { opacity: 0.6 } : null,
      ]}
    >
      <Icon size={size} color={color ?? theme.color.textPrimary} />
    </Pressable>
  );
}
