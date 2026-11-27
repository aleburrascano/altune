import type { PressableStateCallbackType, StyleProp, ViewStyle } from 'react-native';
import { StyleSheet } from 'react-native';

import { spacing, borderWidth, radius } from '@shared/ui/core/theme/tokens';
import type { Theme } from '@shared/ui/core/theme/theme';

type PressableWebState = PressableStateCallbackType & {
  hovered?: boolean;
  focused?: boolean;
};

const itemStyles = StyleSheet.create({
  item: {
    flexDirection: 'row',
    alignItems: 'center',
    borderRadius: radius.sm,
    borderWidth: borderWidth.emphasis,
    paddingVertical: spacing.sm,
    paddingHorizontal: spacing.sm,
    marginBottom: spacing.xs,
  },
});

export function sidebarItemStyle(theme: Theme, state: 'active' | 'idle') {
  return ({ hovered, focused }: PressableWebState): StyleProp<ViewStyle> => [
    itemStyles.item,
    hovered ? { backgroundColor: theme.color.surface2 } : null,
    { borderColor: focused ? theme.color.accent : 'transparent' },
    state === 'active' ? { backgroundColor: theme.color.accentTint } : null,
  ];
}
