import { type ReactElement, type ReactNode } from 'react';
import { Pressable, StyleSheet } from 'react-native';

import { minInteractiveHeight, radius, spacing, useTheme, type Theme } from '@shared/ui/theme';

import { sharedStyles } from './styles';

export type SavePillShellProps = {
  onPress: () => void;
  disabled: boolean;
  interactive: boolean;
  accessibilityLabel: string;
  accessibilityState?: { disabled?: boolean; busy?: boolean };
  testID?: string;
  children: ReactNode;
};

function pillStyle(theme: Theme, interactive: boolean) {
  return ({ pressed }: { pressed: boolean }) => [
    styles.savePill,
    { borderColor: theme.color.border, backgroundColor: theme.color.surface1 },
    pressed && interactive ? sharedStyles.pressed : null,
  ];
}

function shellA11y(props: SavePillShellProps) {
  return {
    accessibilityRole: 'button' as const,
    accessibilityLabel: props.accessibilityLabel,
    accessibilityState: props.accessibilityState,
  };
}

function shellProps(props: SavePillShellProps, theme: Theme) {
  return {
    testID: props.testID,
    onPress: props.onPress,
    disabled: props.disabled,
    style: pillStyle(theme, props.interactive),
    ...shellA11y(props),
  };
}

export function SavePillShell(props: SavePillShellProps): ReactElement {
  const theme = useTheme();
  return <Pressable {...shellProps(props, theme)}>{props.children}</Pressable>;
}

const styles = StyleSheet.create({
  savePill: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.sm,
    minHeight: minInteractiveHeight,
    paddingHorizontal: spacing.lg,
    borderWidth: StyleSheet.hairlineWidth,
    borderRadius: radius.full,
    flexShrink: 0,
  },
});
