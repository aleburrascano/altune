import type { ReactElement } from 'react';
import type { TrackResponse } from '@shared/api-client/trackResponse';
import { countLabel } from '@shared/ui/displayFormat';
import { AddToPlaylistSheet } from '@shared/playlists';
import { ContextMenu } from '@shared/ui/primitives/ContextMenu';
import { useReconcileSelection } from '@features/library/hooks/useSelection';
import type {
  TrackSelectionController,
  SelectionAction,
} from '@features/library/hooks/useTrackSelection';
import { Pressable, ScrollView, StyleSheet, View } from 'react-native';
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

export function SelectionBar({
  count,
  allSelected,
  onSelectAll,
  onCancel,
  actions,
}: SelectionBarProps): ReactElement {
  const theme = useTheme();

  return (
    <View
      testID="selection-bar"
      style={[
        styles.bar,
        { backgroundColor: theme.color.surface1, borderTopColor: theme.color.border },
      ]}
    >
      <View style={styles.header}>
        <Text variant="bodyStrong" testID="selection-count">
          {count} selected
        </Text>
        <View style={styles.headerActions}>
          <Pressable
            testID="selection-select-all"
            onPress={onSelectAll}
            hitSlop={8}
            accessibilityRole="button"
            accessibilityLabel={allSelected ? 'Deselect all tracks' : 'Select all tracks'}
          >
            <Text variant="label" style={{ color: theme.color.accent }}>
              {allSelected ? 'Deselect all' : 'Select all'}
            </Text>
          </Pressable>
          <Pressable
            testID="selection-cancel"
            onPress={onCancel}
            hitSlop={8}
            accessibilityRole="button"
            accessibilityLabel="Exit selection"
          >
            <Text variant="label" tone="secondary">
              Cancel
            </Text>
          </Pressable>
        </View>
      </View>

      <ScrollView
        horizontal
        showsHorizontalScrollIndicator={false}
        contentContainerStyle={styles.actions}
      >
        {actions.map((action) => {
          const Icon = action.icon;
          const color = action.disabled
            ? theme.color.textTertiary
            : action.tone === 'danger'
              ? theme.color.danger
              : theme.color.textPrimary;
          return (
            <Pressable
              key={action.key}
              testID={`selection-action-${action.key}`}
              onPress={action.onPress}
              disabled={action.disabled}
              accessibilityRole="button"
              accessibilityLabel={action.label}
              accessibilityState={{ disabled: action.disabled === true }}
              style={({ pressed }) => [
                styles.action,
                { backgroundColor: theme.color.surface2 },
                pressed && !action.disabled ? styles.pressed : null,
              ]}
            >
              <Icon size={16} color={color} />
              <Text variant="label" style={{ color }}>
                {action.label}
              </Text>
            </Pressable>
          );
        })}
      </ScrollView>
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
