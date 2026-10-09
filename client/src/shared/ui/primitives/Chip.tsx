import type { ReactNode } from 'react';
import { Pressable, View } from 'react-native';

import { tapFeedback } from '@shared/ui/haptics';
import { radius, spacing } from '@shared/ui/theme/tokens';
import { useTheme } from '@shared/ui/theme/useTheme';
import { pressedStyle } from '@shared/ui/primitives/pressedStyle';
import { Text } from '@shared/ui/primitives/Text';
import type { TextTone } from '@shared/ui/primitives/Text';
import { recordTap } from '@shared/ui/primitives/recordTap';

type ChipBase = {
  label: string;
  selected?: boolean;
  icon?: ReactNode;
  testID?: string;
};

export type ChipProps =
  | (ChipBase & { onPress?: undefined; action?: undefined; trackId?: undefined })
  | (ChipBase & { onPress: () => void; action: string; trackId?: string });

type PressableChipProps = ChipBase & { onPress: () => void; action: string; trackId?: string };

const bodyStyle = {
  flexDirection: 'row',
  alignItems: 'center',
  gap: spacing.xs,
  paddingVertical: spacing.md,
  paddingHorizontal: spacing.lg,
  borderRadius: radius.full,
  minHeight: 44,
} as const;

function useChipColors(selected: boolean) {
  const theme = useTheme();
  return {
    backgroundColor: selected ? theme.color.accent : theme.color.surface2,
    tone: selected ? ('onAccent' as const) : ('secondary' as const),
  };
}

function ChipLabel({ label, tone }: { label: string; tone: TextTone }) {
  return (
    <Text variant="label" tone={tone}>
      {label}
    </Text>
  );
}

function ChipBody({ label, selected = false, icon }: ChipBase) {
  const { backgroundColor, tone } = useChipColors(selected);
  return (
    <View style={[bodyStyle, { backgroundColor }]}>
      {icon != null ? icon : null}
      <ChipLabel label={label} tone={tone} />
    </View>
  );
}

function handleChipPress({ onPress, action, trackId }: PressableChipProps) {
  tapFeedback();
  recordTap(action, trackId);
  onPress();
}

const pressableBase = {
  accessibilityRole: 'button',
  hitSlop: 8,
  style: ({ pressed }: { pressed: boolean }) => pressedStyle(pressed),
} as const;

function chipPressProps(props: PressableChipProps) {
  return {
    ...pressableBase,
    testID: props.testID,
    onPress: () => handleChipPress(props),
    accessibilityLabel: props.label,
    accessibilityState: { selected: props.selected ?? false },
  };
}

function PressableChip(props: PressableChipProps) {
  return (
    <Pressable {...chipPressProps(props)}>
      <ChipBody {...props} />
    </Pressable>
  );
}

export function Chip(props: ChipProps) {
  if (props.onPress) {
    return <PressableChip {...props} />;
  }
  return (
    <View testID={props.testID}>
      <ChipBody {...props} />
    </View>
  );
}
