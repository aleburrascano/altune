import { StyleSheet } from 'react-native';
import type { PressableStateCallbackType, StyleProp, ViewStyle } from 'react-native';

import { pressedStyle, spacing, type Theme } from '@shared/ui';

export function rowSurfaceStyle(
  theme: Theme,
  highlight: ViewStyle | null,
): (state: PressableStateCallbackType) => StyleProp<ViewStyle> {
  return ({ pressed }) => [
    styles.row,
    { borderBottomColor: theme.color.border },
    highlight,
    pressedStyle(pressed),
  ];
}

const styles = StyleSheet.create({
  row: {
    paddingVertical: spacing.sm,
    paddingHorizontal: spacing.lg,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
});
