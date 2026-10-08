import type { ReactNode } from 'react';
import { Pressable, View } from 'react-native';

import { tapFeedback } from '../haptics';
import { radius, spacing } from '../theme/tokens';
import { useTheme } from '../theme/useTheme';
import { pressedStyle } from './pressedStyle';
import { Text } from './Text';
import { recordTap } from './recordTap';

type ChipBase = {
  label: string;
  selected?: boolean;
  icon?: ReactNode;
  testID?: string;
};

export type ChipProps =
  | (ChipBase & { onPress?: undefined; action?: undefined; trackId?: undefined })
  | (ChipBase & { onPress: () => void; action?: string; trackId?: string });

export function Chip({
  label,
  onPress,
  action,
  trackId,
  selected = false,
  icon,
  testID,
}: ChipProps) {
  const theme = useTheme();
  const body = (
    <View
      style={{
        flexDirection: 'row',
        alignItems: 'center',
        gap: spacing.xs,
        paddingVertical: spacing.md,
        paddingHorizontal: spacing.lg,
        borderRadius: radius.full,
        backgroundColor: selected ? theme.color.accent : theme.color.surface2,
        minHeight: 44,
      }}
    >
      {icon != null ? icon : null}
      <Text variant="label" tone={selected ? 'onAccent' : 'secondary'}>
        {label}
      </Text>
    </View>
  );

  if (onPress) {
    return (
      <Pressable
        testID={testID}
        onPress={() => {
          tapFeedback();
          recordTap(action, trackId);
          onPress();
        }}
        accessibilityRole="button"
        accessibilityLabel={label}
        accessibilityState={{ selected }}
        hitSlop={8}
        style={({ pressed }) => pressedStyle(pressed)}
      >
        {body}
      </Pressable>
    );
  }
  return <View testID={testID}>{body}</View>;
}
