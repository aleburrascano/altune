import {
  StyleSheet,
  type PressableStateCallbackType,
  type StyleProp,
  type ViewStyle,
} from 'react-native';
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
    pressedStyle(state.pressed),
  ];
}

function wideRowState(args: WideRowArgs): StyleProp<ViewStyle> {
  return [wideStyles.row, wideRowBaseStyle(args.theme, args.state), ...wideRowHoverStyle(args)];
}

export function libraryRowWideStyle(
  theme: Theme,
  isPlaying: boolean,
): (state: WideRowPressableState) => StyleProp<ViewStyle> {
  return (state) => wideRowState({ theme, isPlaying, state });
}

const wideStyles = StyleSheet.create({
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.md,
    paddingVertical: spacing.sm,
    paddingHorizontal: spacing.lg,
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderWidth: 2,
  },
});

export const WIDE_TRACK_COLUMNS = { artwork: 48, duration: 64, status: 96 };
