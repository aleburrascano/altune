import { StyleSheet } from 'react-native';
import type { StyleProp, ViewStyle } from 'react-native';

import { spacing, type Theme } from '@shared/ui';

type WideRowPressableState = {
  pressed: boolean;
  hovered?: boolean;
  focused?: boolean;
};

function wideRowBaseStyle(theme: Theme, state: WideRowPressableState) {
  return {
    borderBottomColor: theme.color.border,
    borderColor: state.focused ? theme.color.accent : 'transparent',
  };
}

type WideRowArgs = { theme: Theme; isPlaying: boolean; state: WideRowPressableState };

function wideRowHoverStyle({ theme, isPlaying, state }: WideRowArgs) {
  return [
    state.hovered ? { backgroundColor: theme.color.surface2 } : null,
    isPlaying ? { backgroundColor: theme.color.accentTint } : null,
    state.pressed ? styles.pressed : null,
  ];
}

function wideRowState(args: WideRowArgs): StyleProp<ViewStyle> {
  return [styles.row, wideRowBaseStyle(args.theme, args.state), ...wideRowHoverStyle(args)];
}

export function libraryRowWideStyle(
  theme: Theme,
  isPlaying: boolean,
): (state: WideRowPressableState) => StyleProp<ViewStyle> {
  return (state) => wideRowState({ theme, isPlaying, state });
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
