import type { PressableStateCallbackType, StyleProp, ViewStyle } from 'react-native';
import { StyleSheet } from 'react-native';

import { spacing } from '../theme/tokens';
import type { Theme } from '../theme/theme';

export type PressableWebState = PressableStateCallbackType & { hovered?: boolean; focused?: boolean };

const styles = StyleSheet.create({
  item: {
    flexDirection: 'row',
    alignItems: 'center',
    borderRadius: 8,
    borderWidth: 2,
    paddingVertical: spacing.sm,
    paddingHorizontal: spacing.sm,
    marginBottom: spacing.xs,
  },
});

export function sidebarItemStyle(theme: Theme, active: boolean) {
  return ({ hovered, focused }: PressableWebState): StyleProp<ViewStyle> => [
    styles.item,
    hovered ? { backgroundColor: theme.color.surface2 } : null,
    { borderColor: focused ? theme.color.accent : 'transparent' },
    active ? { backgroundColor: theme.color.accentTint } : null,
  ];
}
