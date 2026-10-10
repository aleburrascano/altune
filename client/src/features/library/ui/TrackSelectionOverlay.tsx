import type { ReactElement } from 'react';
import type { TrackResponse } from '@shared/apiClient/trackResponse';
import { countLabel } from '@shared/ui/displayFormat';
import { AddToPlaylistSheet } from '@shared/playlists';
import { ContextMenu } from '@shared/ui/primitives/ContextMenu';
import { useReconcileSelection } from '@features/library/hooks/useSelection';
import type {
  TrackSelectionController,
  SelectionAction,
} from '@features/library/hooks/useTrackSelection';
import {
  FlatList,
  Pressable,
  StyleSheet,
  View,
  type ListRenderItemInfo,
  type PressableProps,
} from 'react-native';
import { Text, minInteractiveHeight, radius, spacing, useTheme } from '@shared/ui';

type TrackSelectionOverlayProps = {
  controller: TrackSelectionController;
  tracks: TrackResponse[];
  barVisible: boolean;
};

export function TrackSelectionOverlay({
  controller,
  tracks,
  barVisible,
}: TrackSelectionOverlayProps): ReactElement {
  const { selection, trackAction } = controller;
  useReconcileSelection(selection, tracks);
  const selectedIds = controller.selectedIds(tracks);

  return (
    <>
      {barVisible ? (
        <SelectionBar
          count={selectedIds.length}
          allSelected={controller.allSelected(tracks)}
          onSelectAll={() => controller.toggleSelectAll(tracks)}
          onCancel={selection.clear}
          actions={controller.selectionActionsFor(tracks)}
        />
      ) : null}

      <AddToPlaylistSheet
        visible={controller.bulkSheetVisible}
        label={`${selectedIds.length} ${countLabel(selectedIds.length, 'track')}`}
        resolveTrackIds={() => Promise.resolve(selectedIds)}
        onClose={controller.closeBulkSheet}
      />

      <ContextMenu
        visible={trackAction != null}
        anchor={trackAction?.anchor}
        items={trackAction != null ? controller.trackMenuItems(trackAction.track) : []}
        onClose={controller.closeTrackMenu}
      />
    </>
  );
}

type SelectionBarProps = {
  count: number;
  allSelected: boolean;
  onSelectAll: () => void;
  onCancel: () => void;
  actions: SelectionAction[];
};

const HIT_SLOP = 8;
const ACTION_ICON_SIZE = 16;

type Theme = ReturnType<typeof useTheme>;
type HeaderProps = Pick<SelectionBarProps, 'count' | 'allSelected' | 'onSelectAll' | 'onCancel'>;
type SelectAllProps = Pick<HeaderProps, 'allSelected' | 'onSelectAll'>;
type HeaderActionsProps = Omit<HeaderProps, 'count'>;
type ColoredActionProps = ActionButtonProps & { color: string };
type BarColors = { backgroundColor: string; borderTopColor: string };
type CancelProps = Pick<HeaderProps, 'onCancel'>;
type ActionButtonProps = { action: SelectionAction };

function selectAllProps({ allSelected, onSelectAll }: SelectAllProps): PressableProps {
  return {
    testID: 'selection-select-all',
    onPress: onSelectAll,
    hitSlop: HIT_SLOP,
    accessibilityRole: 'button',
    accessibilityLabel: allSelected ? 'Deselect all tracks' : 'Select all tracks',
  };
}

function SelectAllButton(props: SelectAllProps): ReactElement {
  const theme = useTheme();
  return (
    <Pressable {...selectAllProps(props)}>
      <Text variant="label" style={{ color: theme.color.accent }}>
        {props.allSelected ? 'Deselect all' : 'Select all'}
      </Text>
    </Pressable>
  );
}

function cancelProps({ onCancel }: CancelProps): PressableProps {
  return {
    testID: 'selection-cancel',
    onPress: onCancel,
    hitSlop: HIT_SLOP,
    accessibilityRole: 'button',
    accessibilityLabel: 'Exit selection',
  };
}

function CancelButton(props: CancelProps): ReactElement {
  return (
    <Pressable {...cancelProps(props)}>
      <Text variant="label" tone="secondary">
        Cancel
      </Text>
    </Pressable>
  );
}

function SelectionCount({ count }: Pick<HeaderProps, 'count'>): ReactElement {
  return (
    <Text variant="bodyStrong" testID="selection-count">
      {count} selected
    </Text>
  );
}

function HeaderActions(props: HeaderActionsProps): ReactElement {
  return (
    <View style={styles.headerActions}>
      <SelectAllButton allSelected={props.allSelected} onSelectAll={props.onSelectAll} />
      <CancelButton onCancel={props.onCancel} />
    </View>
  );
}

function SelectionHeader({ count, ...rest }: HeaderProps): ReactElement {
  return (
    <View style={styles.header}>
      <SelectionCount count={count} />
      <HeaderActions {...rest} />
    </View>
  );
}

function actionColor(action: SelectionAction, theme: Theme): string {
  if (action.disabled) return theme.color.textTertiary;
  return action.tone === 'danger' ? theme.color.danger : theme.color.textPrimary;
}

function actionStyle(action: SelectionAction, backgroundColor: string): PressableProps['style'] {
  return ({ pressed }) => [
    styles.action,
    { backgroundColor },
    pressed && !action.disabled ? styles.pressed : null,
  ];
}

function actionA11y(action: SelectionAction): PressableProps {
  return {
    accessibilityRole: 'button',
    accessibilityLabel: action.label,
    accessibilityState: { disabled: action.disabled === true },
  };
}

function actionProps(action: SelectionAction, backgroundColor: string): PressableProps {
  return {
    testID: `selection-action-${action.key}`,
    onPress: action.onPress,
    disabled: action.disabled,
    ...actionA11y(action),
    style: actionStyle(action, backgroundColor),
  };
}

function ActionLabel({ action, color }: ColoredActionProps): ReactElement {
  return (
    <Text variant="label" style={{ color }}>
      {action.label}
    </Text>
  );
}

function ActionContent({ action, color }: ColoredActionProps): ReactElement {
  const Icon = action.icon;
  return (
    <>
      <Icon size={ACTION_ICON_SIZE} color={color} />
      <ActionLabel action={action} color={color} />
    </>
  );
}

function SelectionActionButton({ action }: ActionButtonProps): ReactElement {
  const theme = useTheme();
  const color = actionColor(action, theme);
  return (
    <Pressable {...actionProps(action, theme.color.surface2)}>
      <ActionContent action={action} color={color} />
    </Pressable>
  );
}

function actionKey(action: SelectionAction): string {
  return action.key;
}

function renderActionItem({ item }: ListRenderItemInfo<SelectionAction>): ReactElement {
  return <SelectionActionButton action={item} />;
}

const ACTION_LIST_PROPS = {
  horizontal: true,
  showsHorizontalScrollIndicator: false,
  keyExtractor: actionKey,
  renderItem: renderActionItem,
} as const;

function ActionsRow({ actions }: Pick<SelectionBarProps, 'actions'>): ReactElement {
  return <FlatList {...ACTION_LIST_PROPS} data={actions} contentContainerStyle={styles.actions} />;
}

function barColors(theme: Theme): BarColors {
  return { backgroundColor: theme.color.surface1, borderTopColor: theme.color.border };
}

function SelectionBar({ actions, ...header }: SelectionBarProps): ReactElement {
  const theme = useTheme();
  return (
    <View testID="selection-bar" style={[styles.bar, barColors(theme)]}>
      <SelectionHeader {...header} />
      <ActionsRow actions={actions} />
    </View>
  );
}

const styles = StyleSheet.create({
  bar: {
    borderTopWidth: StyleSheet.hairlineWidth,
    paddingTop: spacing.md,
    paddingBottom: spacing.md,
    gap: spacing.md,
  },
  header: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    paddingHorizontal: spacing.lg,
  },
  headerActions: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.lg,
  },
  actions: {
    flexDirection: 'row',
    gap: spacing.sm,
    paddingHorizontal: spacing.lg,
  },
  action: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.xs,
    minHeight: minInteractiveHeight,
    paddingHorizontal: spacing.md,
    borderRadius: radius.full,
  },
  pressed: { opacity: 0.6 },
});
