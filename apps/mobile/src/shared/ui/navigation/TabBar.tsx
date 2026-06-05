import type { BottomTabBarProps } from '@react-navigation/bottom-tabs';
import { Compass, Library as LibraryIcon } from 'lucide-react-native';
import type { ComponentType } from 'react';
import { Pressable, StyleSheet, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { Text } from '../primitives/Text';
import { spacing } from '../theme/tokens';
import { useTheme } from '../theme/useTheme';

type IconComponent = ComponentType<{ size?: number; color?: string }>;

const ICONS: Record<string, IconComponent> = {
  discover: Compass,
  library: LibraryIcon,
};

// AIDEV-NOTE: docked tab bar — flush to the bottom edge, hairline top border,
// active tab marked by a 2px accent indicator (no pill, no glass blur). The
// gap above this bar still hosts a future mini-player without touching the tabs.
export function TabBar({ state, descriptors, navigation }: BottomTabBarProps) {
  const theme = useTheme();
  const insets = useSafeAreaInsets();

  const items = state.routes
    .map((route, index) => {
      const descriptor = descriptors[route.key];
      // Skip routes with href: null (hidden from tab bar but still navigable).
      // Expo Router extends BottomTabNavigationOptions with href — cast to access it.
      // Also skip routes not in our ICONS map (e.g., 'detail').
      const opts = descriptor?.options as { href?: string | null } | undefined;
      if (opts?.href === null || !(route.name in ICONS)) {
        return null;
      }

      const focused = state.index === index;
      const label =
        typeof descriptor?.options.title === 'string' ? descriptor.options.title : route.name;
      const Icon = ICONS[route.name] ?? Compass;
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

      return (
        <Pressable
          key={route.key}
          onPress={onPress}
          accessibilityRole="button"
          accessibilityState={{ selected: focused }}
          accessibilityLabel={label}
          hitSlop={8}
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
        </Pressable>
      );
    })
    .filter(Boolean);

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
    opacity: 0.7,
  },
  indicator: {
    width: 28,
    height: 2,
    borderRadius: 2,
    marginBottom: spacing.sm,
  },
});
