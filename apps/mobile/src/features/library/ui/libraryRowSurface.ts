import { StyleSheet } from 'react-native';
import type { PressableStateCallbackType, StyleProp, ViewStyle } from 'react-native';

import { spacing, type Theme } from '@shared/ui';

/** The row surface both library-row modes press on; `highlight` is the selected tint. */
export function rowSurfaceStyle(
  theme: Theme,
  highlight: ViewStyle | null,
): (state: PressableStateCallbackType) => StyleProp<ViewStyle> {
  return ({ pressed }) => [
    styles.row,
    { borderBottomColor: theme.color.border },
    highlight,
    pressed ? styles.pressed : null,
  ];
}

const styles = StyleSheet.create({
  row: {
    paddingVertical: spacing.sm,
    paddingHorizontal: spacing.lg,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  pressed: { opacity: 0.7 },
});
