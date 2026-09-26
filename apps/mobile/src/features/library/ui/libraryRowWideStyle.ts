import { StyleSheet } from 'react-native';
import type { PressableStateCallbackType, StyleProp, ViewStyle } from 'react-native';

import { spacing, type Theme } from '@shared/ui';

export type WideRowPressableState = PressableStateCallbackType & {
  hovered?: boolean;
  focused?: boolean;
};

function wideRowState(theme: Theme, isPlaying: boolean, state: WideRowPressableState): StyleProp<ViewStyle> {
  return [
    styles.row,
    { borderBottomColor: theme.color.border, borderColor: state.focused ? theme.color.accent : 'transparent' },
    state.hovered ? { backgroundColor: theme.color.surface2 } : null,
    isPlaying ? { backgroundColor: theme.color.accentTint } : null,
    state.pressed ? styles.pressed : null,
  ];
}

export function libraryRowWideStyle(
  theme: Theme,
  isPlaying: boolean,
): (state: WideRowPressableState) => StyleProp<ViewStyle> {
  return (state) => wideRowState(theme, isPlaying, state);
}

const styles = StyleSheet.create({
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.md,
    paddingVertical: spacing.sm,
    paddingHorizontal: spacing.lg,
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderWidth: 2,
  },
  pressed: { opacity: 0.7 },
});
