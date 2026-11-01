import { useState, type ComponentProps, type ReactElement } from 'react';
import { StyleSheet, View, type PressableStateCallbackType } from 'react-native';
import { ChevronDown } from 'lucide-react-native';
import { spacing } from '@shared/ui/theme/tokens';
import { useTheme } from '@shared/ui/theme/useTheme';
import { pressedStyle } from '@shared/ui/primitives/pressedStyle';
import { Text } from '@shared/ui/primitives/Text';
import { Tappable } from '@shared/ui/primitives/Tappable';
import { ActionSheet, type ActionSheetOption } from '@shared/ui/primitives/ActionSheet';

export type SortOption<K extends string> = { key: K; label: string };

export type SortControlProps<K extends string> = {
  count: number;
  noun: string;
  sortKey: K;
  options: SortOption<K>[];
  onSortChange: (key: K) => void;
  action: string;
  testID: string;
};

function activeLabelOf<K extends string>(options: SortOption<K>[], sortKey: K): string {
  const active = options.find((o) => o.key === sortKey) ?? options[0];
  return active?.label ?? '';
}

function countLabel(count: number, noun: string): string {
  return `${count} ${count === 1 ? noun : `${noun}s`}`;
}

type SortMenuProps<K extends string> = Omit<SortControlProps<K>, 'count' | 'noun'>;

type TappableProps = Omit<ComponentProps<typeof Tappable>, 'children'>;

type SortTriggerProps<K extends string> = SortMenuProps<K> & { onOpen: () => void };

type SortSheetProps<K extends string> = SortMenuProps<K> & {
  visible: boolean;
  onClose: () => void;
};

function triggerStyle({ pressed }: PressableStateCallbackType) {
  return [styles.sort, pressedStyle(pressed)];
}

const TRIGGER_BASE: Pick<TappableProps, 'hitSlop' | 'accessibilityRole' | 'style'> = {
  hitSlop: 8,
  accessibilityRole: 'button',
  style: triggerStyle,
};

function toSheetOptions<K extends string>(props: SortMenuProps<K>): ActionSheetOption[] {
  return props.options.map((o) => ({
    label: o.label,
    testID: `${props.testID}-${o.key}`,
    onPress: () => props.onSortChange(o.key),
  }));
}

type SortLabelProps = { label: string };

function SortLabel({ label }: SortLabelProps): ReactElement {
  const theme = useTheme();
  return (
    <>
      <Text variant="label">{label}</Text>
      <ChevronDown size={14} color={theme.color.textPrimary} />
    </>
  );
}

function triggerProps<K extends string>(props: SortTriggerProps<K>, label: string): TappableProps {
  return {
    ...TRIGGER_BASE,
    action: props.action,
    testID: props.testID,
    onPress: props.onOpen,
    accessibilityLabel: `Sort by ${label}`,
  };
}

function SortTrigger<K extends string>(props: SortTriggerProps<K>): ReactElement {
  const label = activeLabelOf(props.options, props.sortKey);
  return (
    <Tappable {...triggerProps(props, label)}>
      <SortLabel label={label} />
    </Tappable>
  );
}

function SortSheet<K extends string>(props: SortSheetProps<K>): ReactElement {
  return (
    <ActionSheet
      visible={props.visible}
      title="Sort by"
      options={toSheetOptions(props)}
      onClose={props.onClose}
    />
  );
}

function SortMenu<K extends string>(props: SortMenuProps<K>): ReactElement {
  const [open, setOpen] = useState(false);
  return (
    <>
      <SortTrigger {...props} onOpen={() => setOpen(true)} />
      <SortSheet {...props} visible={open} onClose={() => setOpen(false)} />
    </>
  );
}

export function SortControl<K extends string>(props: SortControlProps<K>): ReactElement {
  return (
    <View style={styles.row}>
      <Text variant="label" tone="tertiary">
        {countLabel(props.count, props.noun)}
      </Text>
      <SortMenu {...props} />
    </View>
  );
}

const SORT_CONTROL_MIN_HEIGHT = 32;

const styles = StyleSheet.create({
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    paddingTop: spacing.md,
    paddingBottom: spacing.sm,
  },
  sort: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.xs,
    minHeight: SORT_CONTROL_MIN_HEIGHT,
  },
});
