import type { ReactElement } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';

import { Text } from '@shared/ui/primitives/Text';
import { radius, spacing, useTheme } from '@shared/ui/theme';

import type { RecordTypeGroup } from '../hooks/useDiscographyFilter';
import { sharedStyles } from './styles';

type ChipProps = { group: RecordTypeGroup; on: boolean; onSelect: (type: string) => void };

function chipStyle(theme: ReturnType<typeof useTheme>, on: boolean) {
  return ({ pressed }: { pressed: boolean }) => [
    styles.chip,
    { backgroundColor: on ? theme.color.accent : theme.color.surface2, borderColor: on ? 'transparent' : theme.color.border },
    pressed ? sharedStyles.pressed : null,
  ];
}

function chipPressableProps(props: ChipProps, theme: ReturnType<typeof useTheme>) {
  return {
    testID: `detail-discography-${props.group.type}`,
    onPress: () => props.onSelect(props.group.type),
    accessibilityRole: 'button' as const,
    accessibilityLabel: `${props.group.label}, ${props.group.count}`,
    accessibilityState: { selected: props.on },
    style: chipStyle(theme, props.on),
  };
}

function Chip(props: ChipProps): ReactElement {
  const theme = useTheme();
  return (
    <Pressable {...chipPressableProps(props, theme)}>
      <Text variant="label" tone={props.on ? 'onAccent' : 'secondary'}>
        {props.group.label} {props.group.count}
      </Text>
    </Pressable>
  );
}

export type RecordTypeChipsProps = {
  present: readonly RecordTypeGroup[];
  active: RecordTypeGroup;
  onSelect: (type: string) => void;
};

export function RecordTypeChips(props: RecordTypeChipsProps): ReactElement | null {
  if (props.present.length <= 1) return null;
  return (
    <View style={styles.chips}>
      {props.present.map((group) => (
        <Chip key={group.type} group={group} on={group.type === props.active.type} onSelect={props.onSelect} />
      ))}
    </View>
  );
}

const styles = StyleSheet.create({
  chips: { flexDirection: 'row', gap: spacing.sm, marginBottom: spacing.md, flexWrap: 'wrap' },
  chip: {
    minHeight: 36,
    paddingHorizontal: spacing.md,
    justifyContent: 'center',
    borderRadius: radius.full,
    borderWidth: StyleSheet.hairlineWidth,
  },
});
