import { useState, type ReactElement } from 'react';
import { Animated, Pressable, StyleSheet, View } from 'react-native';
import { ChevronLeft, MoreHorizontal, type LucideIcon } from 'lucide-react-native';

import { Text } from '@shared/ui/primitives/Text';
import { ContextMenu, type ContextMenuItem } from '@shared/ui/primitives/ContextMenu';
import { spacing, useTheme } from '@shared/ui/theme';

import { withAlpha } from './color';
import { sharedStyles } from './styles';

type GlassButtonProps = {
  testID: string;
  onPress: () => void;
  accessibilityLabel: string;
  icon: LucideIcon;
  iconSize: number;
};

function glassStyle(canvas: string) {
  return ({ pressed }: { pressed: boolean }) => [
    styles.glass,
    { backgroundColor: withAlpha(canvas, 0.42) },
    pressed ? sharedStyles.pressed : null,
  ];
}

function glassPressableProps(props: Omit<GlassButtonProps, 'icon'>, canvas: string) {
  return {
    testID: props.testID,
    onPress: props.onPress,
    accessibilityRole: 'button' as const,
    accessibilityLabel: props.accessibilityLabel,
    hitSlop: 8,
    style: glassStyle(canvas),
  };
}

function GlassButton({ icon: Icon, ...props }: GlassButtonProps): ReactElement {
  const theme = useTheme();
  return (
    <Pressable {...glassPressableProps(props, theme.color.canvas)}>
      <Icon size={props.iconSize} color={theme.color.textPrimary} />
    </Pressable>
  );
}

const BACK_GLASS = {
  testID: 'detail-back',
  accessibilityLabel: 'Go back',
  icon: ChevronLeft,
  iconSize: 22,
} as const;

const MENU_GLASS = {
  testID: 'detail-menu',
  accessibilityLabel: 'More options',
  icon: MoreHorizontal,
  iconSize: 20,
} as const;

type FadingTitleProps = { title: string; opacity: Animated.AnimatedInterpolation<number> };

function fadingTitleProps() {
  return {
    pointerEvents: 'none' as const,
    accessibilityElementsHidden: true,
    importantForAccessibility: 'no-hide-descendants' as const,
  };
}

function FadingTitle({ title, opacity }: FadingTitleProps): ReactElement {
  return (
    <Animated.View style={[styles.barTitle, { opacity }]} {...fadingTitleProps()}>
      <Text variant="bodyStrong" numberOfLines={1}>
        {title}
      </Text>
    </Animated.View>
  );
}

type MenuButtonProps = { hasMenu: boolean; onOpenMenu: () => void };

function MenuButton(props: MenuButtonProps): ReactElement {
  if (!props.hasMenu) return <View style={styles.glassSpacer} />;
  return <GlassButton {...MENU_GLASS} onPress={props.onOpenMenu} />;
}

type TopBarRowProps = {
  title: string;
  onBack: () => void;
  top: number;
  height: number;
  chromeOpacity: Animated.AnimatedInterpolation<number>;
  hasMenu: boolean;
  onOpenMenu: () => void;
};

function TopBarRow(props: TopBarRowProps): ReactElement {
  return (
    <View style={[styles.bar, { top: props.top, height: props.height }]}>
      <GlassButton {...BACK_GLASS} onPress={props.onBack} />
      <FadingTitle title={props.title} opacity={props.chromeOpacity} />
      <MenuButton hasMenu={props.hasMenu} onOpenMenu={props.onOpenMenu} />
    </View>
  );
}

export type DetailTopBarProps = {
  title: string;
  onBack: () => void;
  menuItems?: ContextMenuItem[] | undefined;
  top: number;
  height: number;
  chromeOpacity: Animated.AnimatedInterpolation<number>;
};

type DetailMenuState = {
  hasMenu: boolean;
  menuOpen: boolean;
  onOpenMenu: () => void;
  onClose: () => void;
};

function useDetailMenu(menuItems: ContextMenuItem[] | undefined): DetailMenuState {
  const [menuOpen, setMenuOpen] = useState(false);
  return {
    hasMenu: menuItems != null && menuItems.length > 0,
    menuOpen,
    onOpenMenu: () => setMenuOpen(true),
    onClose: () => setMenuOpen(false),
  };
}

function topBarRowProps(props: DetailTopBarProps, menu: DetailMenuState): TopBarRowProps {
  return { ...props, hasMenu: menu.hasMenu, onOpenMenu: menu.onOpenMenu };
}

type DetailMenuOverlayProps = { props: DetailTopBarProps; menu: DetailMenuState };

function contextMenuProps({ props, menu }: DetailMenuOverlayProps) {
  return {
    visible: menu.menuOpen,
    items: props.menuItems ?? [],
    anchorTop: props.top + props.height,
    onClose: menu.onClose,
  };
}

function DetailMenuOverlay(props: DetailMenuOverlayProps): ReactElement | null {
  if (!props.menu.hasMenu) return null;
  return <ContextMenu {...contextMenuProps(props)} />;
}

export function DetailTopBar(props: DetailTopBarProps): ReactElement {
  const menu = useDetailMenu(props.menuItems);
  return (
    <>
      <TopBarRow {...topBarRowProps(props, menu)} />
      <DetailMenuOverlay props={props} menu={menu} />
    </>
  );
}

const GLASS = 40;

const styles = StyleSheet.create({
  bar: {
    position: 'absolute',
    left: spacing.md,
    right: spacing.md,
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.sm,
  },
  barTitle: { flex: 1, alignItems: 'center' },
  glass: {
    width: GLASS,
    height: GLASS,
    borderRadius: GLASS / 2,
    alignItems: 'center',
    justifyContent: 'center',
  },
  glassSpacer: { width: GLASS, height: GLASS },
});
