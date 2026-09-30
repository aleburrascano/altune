import type { ComponentProps, ReactElement } from 'react';
import { ActivityIndicator, Pressable, StyleSheet } from 'react-native';

import { countLabel } from '@shared/lib/format';
import {
  Text,
  minInteractiveHeight,
  pressedStyle,
  radius,
  spacing,
  useTheme,
  type Theme,
} from '@shared/ui';

type ConfirmButtonProps = {
  playlistName: string;
  count: number;
  canConfirm: boolean;
  isEmpty: boolean;
  adding: boolean;
  onPress: () => void;
};

type PressableA11y = Pick<
  ComponentProps<typeof Pressable>,
  'onPress' | 'disabled' | 'accessibilityRole' | 'accessibilityLabel' | 'accessibilityState'
>;

function confirmA11y(props: ConfirmButtonProps): PressableA11y {
  return {
    onPress: props.onPress,
    disabled: !props.canConfirm,
    accessibilityRole: 'button',
    accessibilityLabel: `Add ${props.count} tracks to ${props.playlistName}`,
    accessibilityState: { disabled: !props.canConfirm },
  };
}

function confirmBackground(theme: Theme, canConfirm: boolean): string {
  return canConfirm ? theme.color.accent : theme.color.surface2;
}

function confirmStyle(theme: Theme, canConfirm: boolean, pressed: boolean) {
  const background = confirmBackground(theme, canConfirm);
  return [styles.confirm, { backgroundColor: background }, pressedStyle(pressed && canConfirm)];
}

function confirmPressableProps(
  theme: Theme,
  props: ConfirmButtonProps,
): ComponentProps<typeof Pressable> {
  return {
    testID: 'add-tracks-confirm',
    ...confirmA11y(props),
    style: ({ pressed }) => confirmStyle(theme, props.canConfirm, pressed),
  };
}

function confirmLabel(props: ConfirmButtonProps): string {
  if (props.isEmpty) return 'Select tracks';
  return `Add ${props.count} ${countLabel(props.count, 'track')}`;
}

type ConfirmContentProps = ConfirmButtonProps & { theme: Theme };

function ConfirmContent(props: ConfirmContentProps): ReactElement {
  if (props.adding) return <ActivityIndicator size="small" color={props.theme.color.onAccent} />;
  const color = props.isEmpty ? props.theme.color.textTertiary : props.theme.color.onAccent;
  return (
    <Text variant="label" style={{ color }}>
      {confirmLabel(props)}
    </Text>
  );
}

export function ConfirmButton(props: ConfirmButtonProps): ReactElement {
  const theme = useTheme();
  return (
    <Pressable {...confirmPressableProps(theme, props)}>
      <ConfirmContent {...props} theme={theme} />
    </Pressable>
  );
}

const styles = StyleSheet.create({
  confirm: {
    minHeight: minInteractiveHeight,
    justifyContent: 'center',
    paddingHorizontal: spacing.xl,
    borderRadius: radius.full,
  },
});
