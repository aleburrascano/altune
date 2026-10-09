import type { ComponentProps, ReactElement } from 'react';
import { Pressable } from 'react-native';

import { Text, useTheme, type Theme } from '@shared/ui';

type SelectAllButtonProps = {
  allSelected: boolean;
  disabled: boolean;
  onPress: () => void;
};

function selectAllPressableProps(props: SelectAllButtonProps): ComponentProps<typeof Pressable> {
  return {
    testID: 'add-tracks-select-all',
    onPress: props.onPress,
    disabled: props.disabled,
    hitSlop: 8,
    accessibilityRole: 'button',
    accessibilityLabel: props.allSelected ? 'Deselect all tracks' : 'Select all tracks',
  };
}

function selectAllColor(theme: Theme, disabled: boolean): string {
  return disabled ? theme.color.textTertiary : theme.color.accent;
}

function selectAllLabel(allSelected: boolean): string {
  return allSelected ? 'Deselect all' : 'Select all';
}

export function SelectAllButton(props: SelectAllButtonProps): ReactElement {
  const theme = useTheme();
  return (
    <Pressable {...selectAllPressableProps(props)}>
      <Text variant="label" style={{ color: selectAllColor(theme, props.disabled) }}>
        {selectAllLabel(props.allSelected)}
      </Text>
    </Pressable>
  );
}
