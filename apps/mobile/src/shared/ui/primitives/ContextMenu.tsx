import { useCallback, type ReactElement } from 'react';
import { Modal, Pressable, StyleSheet, useWindowDimensions, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { Text } from './Text';
import { resolveMenuPlacement, type MenuAnchor } from './menuPlacement';
import { useTheme } from '../theme/useTheme';
import { radius, spacing } from '../theme/tokens';

export type ContextMenuItem = {
  label: string;
  onPress: () => void;
  tone?: 'default' | 'danger';
};

type ContextMenuProps = {
  visible: boolean;
  items: ContextMenuItem[];
  onClose: () => void;
  // Legacy fixed positioning (used by the playlist header menu).
  anchorRight?: number;
  anchorTop?: number;
  // Measured trigger rect (used by row menus). When set, the menu floats next to
  // the trigger and flips above it when there isn't room below — overrides the
  // fixed anchorRight/anchorTop.
  anchor?: MenuAnchor | undefined;
};

export function ContextMenu({
  visible,
  items,
  onClose,
  anchorRight = spacing.lg,
  anchorTop = 52,
  anchor,
}: ContextMenuProps): ReactElement | null {
  const theme = useTheme();
  const { height: windowHeight } = useWindowDimensions();
  const insets = useSafeAreaInsets();

  const handlePress = useCallback(
    (item: ContextMenuItem) => {
      onClose();
      item.onPress();
    },
    [onClose],
  );

  if (!visible) return null;

  const position = anchor
    ? resolveMenuPlacement({
        anchor,
        itemCount: items.length,
        windowHeight,
        insetBottom: insets.bottom,
      })
    : { right: anchorRight, top: anchorTop };

  return (
    <Modal transparent animationType="fade" visible onRequestClose={onClose}>
      <Pressable style={styles.backdrop} onPress={onClose}>
        <View />
      </Pressable>
      <View
        style={[
          styles.menu,
          position,
          {
            backgroundColor: theme.color.surface2,
            borderColor: theme.color.border,
          },
        ]}
      >
        {items.map((item, i) => (
          <Pressable
            key={item.label}
            onPress={() => handlePress(item)}
            style={({ pressed }) => [
              styles.item,
              i < items.length - 1 ? { borderBottomWidth: StyleSheet.hairlineWidth, borderBottomColor: theme.color.border } : null,
              pressed ? { backgroundColor: theme.color.surface1 } : null,
            ]}
            accessibilityRole="button"
            accessibilityLabel={item.label}
          >
            <Text
              variant="body"
              style={item.tone === 'danger' ? { color: theme.color.danger } : undefined}
            >
              {item.label}
            </Text>
          </Pressable>
        ))}
      </View>
    </Modal>
  );
}

const styles = StyleSheet.create({
  backdrop: {
    ...StyleSheet.absoluteFillObject,
  },
  menu: {
    position: 'absolute',
    minWidth: 180,
    borderRadius: radius.lg,
    borderWidth: 1,
    overflow: 'hidden',
  },
  item: {
    paddingHorizontal: spacing.lg,
    paddingVertical: spacing.md,
  },
});
