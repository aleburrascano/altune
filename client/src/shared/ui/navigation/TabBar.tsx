import type { BottomTabBarProps } from 'expo-router/js-tabs';
import { Pressable, StyleSheet, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { Text } from '@shared/ui/primitives/Text';
import { spacing, opacity } from '@shared/ui/theme/tokens';
import { useTheme } from '@shared/ui/theme/useTheme';
import {
  TAB_ROUTE_INFO_BY_ROUTE,
  TAB_ROUTES,
  type TabRoute,
} from '@shared/ui/navigation/tabRoutes';

export function TabBar({ state, descriptors, navigation }: BottomTabBarProps) {
  const theme = useTheme();
  const insets = useSafeAreaInsets();

  const items = state.routes.flatMap((route, index) => {
    const descriptor = descriptors[route.key];
    const opts = descriptor?.options as { href?: string | null } | undefined;
    const isTabRoute = TAB_ROUTES.includes(route.name as TabRoute);
    if (opts?.href === null || !isTabRoute) {
      return [];
    }
    const routeInfo = TAB_ROUTE_INFO_BY_ROUTE[route.name as TabRoute];

    const focused = state.index === index;
    const label =
      typeof descriptor?.options.title === 'string' ? descriptor.options.title : routeInfo.label;
    const Icon = routeInfo.Icon;
    const color = focused ? theme.color.accent : theme.color.textSecondary;

    const onPress = () => {
      const event = navigation.emit({
        type: 'tabPress',
        target: route.key,
        canPreventDefault: true,
      });
      if (!focused && !event.defaultPrevented) {
        navigation.navigate(route.name);
      }
    };

    return [
      <Pressable
        key={route.key}
        onPress={onPress}
        accessibilityRole="button"
        accessibilityState={{ selected: focused }}
        accessibilityLabel={label}
        hitSlop={12}
        style={({ pressed }) => [styles.tab, pressed ? styles.tabPressed : null]}
      >
        <View
          style={[
            styles.indicator,
            { backgroundColor: focused ? theme.color.accent : 'transparent' },
          ]}
        />
        <Icon size={22} color={color} />
        <Text variant="caption" style={{ color, marginTop: spacing.xs }}>
          {label}
        </Text>
      </Pressable>,
    ];
  });

  const bottomPad = insets.bottom > 0 ? insets.bottom : spacing.md;

  return (
    <View
      style={[
        styles.bar,
        {
          backgroundColor: theme.color.canvas,
          borderTopColor: theme.color.border,
          paddingBottom: bottomPad,
        },
      ]}
    >
      {items}
    </View>
  );
}

const INDICATOR_WIDTH = 28;
const INDICATOR_HEIGHT = 2;

const styles = StyleSheet.create({
  bar: {
    flexDirection: 'row',
    borderTopWidth: StyleSheet.hairlineWidth,
    paddingTop: spacing.sm,
  },
  tab: {
    flex: 1,
    alignItems: 'center',
    justifyContent: 'center',
    paddingVertical: spacing.xs,
  },
  tabPressed: {
    opacity: opacity.dimmed,
  },
  indicator: {
    width: INDICATOR_WIDTH,
    height: INDICATOR_HEIGHT,
    borderRadius: INDICATOR_HEIGHT,
    marginBottom: spacing.sm,
  },
});
