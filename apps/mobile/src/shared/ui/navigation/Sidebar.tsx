import type { ReactNode } from 'react';
import type { PressableStateCallbackType, StyleProp, ViewStyle } from 'react-native';
import { Pressable, StyleSheet, View } from 'react-native';

import { Text } from '../primitives/Text';
import { spacing } from '../theme/tokens';
import type { Theme } from '../theme/theme';
import { useTheme } from '../theme/useTheme';
import {
  TAB_ROUTE_INFO_BY_ROUTE,
  TAB_ROUTES,
  type TabIcon,
  type TabRoute,
  type TabRouteInfo,
} from './tabRoutes';

export type SidebarProps = {
  activeRoute: string;
  onNavigate: (route: TabRoute) => void;
  playlists?: ReactNode;
};

function sidebarStyle(theme: Theme): StyleProp<ViewStyle> {
  return [styles.sidebar, { backgroundColor: theme.color.canvas, borderRightColor: theme.color.border }];
}

type PressableWebState = PressableStateCallbackType & { hovered?: boolean; focused?: boolean };

function itemStyle(theme: Theme, active: boolean) {
  return ({ hovered, focused }: PressableWebState): StyleProp<ViewStyle> => [
    styles.item,
    hovered ? { backgroundColor: theme.color.surface2 } : null,
    { borderColor: focused ? theme.color.accent : 'transparent' },
    active ? { backgroundColor: theme.color.accentTint } : null,
  ];
}

type SidebarItemPropsInput = {
  theme: Theme;
  route: TabRoute;
  label: string;
  active: boolean;
  onPress: () => void;
};

function sidebarItemProps({ theme, route, label, active, onPress }: SidebarItemPropsInput) {
  return {
    testID: `sidebar-item-${route}`,
    onPress,
    accessibilityRole: 'button' as const,
    accessibilityLabel: label,
    accessibilityState: { selected: active },
    style: itemStyle(theme, active),
  };
}

function SidebarItemLabel({ Icon, label, color }: { Icon: TabIcon; label: string; color: string }) {
  return (
    <>
      <Icon size={20} color={color} />
      <Text variant="body" style={{ color, marginLeft: spacing.sm }}>
        {label}
      </Text>
    </>
  );
}

type SidebarItemProps = {
  route: TabRoute;
  label: string;
  Icon: TabIcon;
  active: boolean;
  onPress: () => void;
};

function SidebarItem({ route, label, Icon, active, onPress }: SidebarItemProps) {
  const theme = useTheme();
  const color = active ? theme.color.accent : theme.color.textSecondary;
  return (
    <Pressable {...sidebarItemProps({ theme, route, label, active, onPress })}>
      <SidebarItemLabel Icon={Icon} label={label} color={color} />
    </Pressable>
  );
}

type SidebarRouteProps = {
  info: TabRouteInfo;
  active: boolean;
  onPress: () => void;
  playlists?: ReactNode;
};

function SidebarPlaylistsSlot({ show, children }: { show: boolean; children?: ReactNode }) {
  if (!show) return null;
  return (
    <View testID="sidebar-playlists" style={styles.playlists}>
      {children}
    </View>
  );
}

function sidebarItemPropsFrom(info: TabRouteInfo, active: boolean, onPress: () => void) {
  return { route: info.route, label: info.label, Icon: info.Icon, active, onPress };
}

function SidebarRoute({ info, active, onPress, playlists }: SidebarRouteProps) {
  return (
    <View testID={`sidebar-route-${info.route}`}>
      <SidebarItem {...sidebarItemPropsFrom(info, active, onPress)} />
      <SidebarPlaylistsSlot show={info.route === 'library'}>{playlists}</SidebarPlaylistsSlot>
    </View>
  );
}

type RenderSidebarRouteArgs = {
  route: TabRoute;
  activeRoute: string;
  onNavigate: (route: TabRoute) => void;
  playlists?: ReactNode;
};

function renderSidebarRoute({ route, activeRoute, onNavigate, playlists }: RenderSidebarRouteArgs) {
  const onPress = () => onNavigate(route);
  const info = TAB_ROUTE_INFO_BY_ROUTE[route];
  return <SidebarRoute key={route} info={info} active={activeRoute === route} onPress={onPress} playlists={playlists} />;
}

export function Sidebar({ activeRoute, onNavigate, playlists }: SidebarProps) {
  const theme = useTheme();
  return (
    <View testID="sidebar" style={sidebarStyle(theme)}>
      {TAB_ROUTES.map((route) => renderSidebarRoute({ route, activeRoute, onNavigate, playlists }))}
    </View>
  );
}

const styles = StyleSheet.create({
  sidebar: {
    width: 240,
    borderRightWidth: StyleSheet.hairlineWidth,
    paddingTop: spacing.lg,
    paddingHorizontal: spacing.sm,
  },
  item: {
    flexDirection: 'row',
    alignItems: 'center',
    borderRadius: 8,
    borderWidth: 2,
    paddingVertical: spacing.sm,
    paddingHorizontal: spacing.sm,
    marginBottom: spacing.xs,
  },
  playlists: {
    marginBottom: spacing.md,
    marginLeft: spacing.lg,
  },
});
