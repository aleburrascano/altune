import type { ReactElement, ComponentProps } from 'react';
import { StyleSheet, View, Pressable, ActivityIndicator } from 'react-native';
import { ChevronLeft } from 'lucide-react-native';
import {
  Text,
  spacing,
  minInteractiveHeight,
  useTheme,
  type Theme,
  pressedStyle,
  radius,
} from '@shared/ui';
import { IconButton } from '@shared/ui/primitives/IconButton';
import { countLabel } from '@shared/ui/displayFormat';

type AddTracksHeaderProps = {
  playlistName: string;
  onBack: () => void;
};

export function AddTracksHeader({ playlistName, onBack }: AddTracksHeaderProps): ReactElement {
  return (
    <View style={styles.header}>
      <IconButton
        action="library.add_tracks.back"
        icon={ChevronLeft}
        size={24}
        onPress={onBack}
        accessibilityLabel="Back"
      />
      <Text variant="title" numberOfLines={1} style={styles.headerTitle}>
        Add to {playlistName}
      </Text>
    </View>
  );
}

const styles = StyleSheet.create({
  header: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.sm,
    paddingHorizontal: spacing.lg,
    paddingBottom: spacing.sm,
  },
  headerTitle: { flex: 1 },
});

type AlreadyAddedRowProps = {
  title: string;
};

const alreadyAddedStyles = StyleSheet.create({
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: spacing.md,
    minHeight: minInteractiveHeight,
    paddingHorizontal: spacing.lg,
  },
  title: { flex: 1 },
});

const TITLE_PROPS = {
  variant: 'body',
  numberOfLines: 1,
  tone: 'tertiary',
  style: alreadyAddedStyles.title,
} as const;
const ADDED_PROPS = { variant: 'caption', tone: 'tertiary' } as const;

export function AlreadyAddedRow({ title }: AlreadyAddedRowProps): ReactElement {
  return (
    <View style={alreadyAddedStyles.row}>
      <Text {...TITLE_PROPS}>{title}</Text>
      <Text {...ADDED_PROPS}>Added</Text>
    </View>
  );
}

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
  return [
    confirmStyles.confirm,
    { backgroundColor: background },
    pressedStyle(pressed && canConfirm),
  ];
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

const confirmStyles = StyleSheet.create({
  confirm: {
    minHeight: minInteractiveHeight,
    justifyContent: 'center',
    paddingHorizontal: spacing.xl,
    borderRadius: radius.full,
  },
});
