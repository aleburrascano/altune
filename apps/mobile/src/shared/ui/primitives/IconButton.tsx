import type { ComponentType } from 'react';
import { Pressable } from 'react-native';

import { useTheme } from '@shared/ui/theme/useTheme';
import { recordTap } from '@shared/ui/primitives/recordTap';

export type IconButtonProps = {
  action?: string;
  trackId?: string | undefined;
  icon: ComponentType<{ size?: number; color?: string }>;
  onPress: () => void;
  accessibilityLabel: string;
  size?: number;
  color?: string;
  disabled?: boolean;
  testID?: string;
};

export function IconButton({
  action,
  trackId,
  icon: Icon,
  onPress,
  accessibilityLabel,
  size = 24,
  color,
  disabled = false,
  testID,
}: IconButtonProps) {
  const theme = useTheme();
  const handlePress = () => {
    if (disabled) {
      return;
    }
    recordTap(action, trackId);
    onPress();
  };
  return (
    <Pressable
      testID={testID}
      onPress={handlePress}
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
      <Icon
        size={size}
        color={color ?? (disabled ? theme.color.textTertiary : theme.color.textPrimary)}
      />
    </Pressable>
  );
}
