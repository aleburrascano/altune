import type { BottomTabBarProps } from '@react-navigation/bottom-tabs';
import { Compass, Library as LibraryIcon, Settings } from 'lucide-react-native';
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
  settings: Settings,
};

// AIDEV-NOTE: docked tab bar — flush to the bottom edge, hairline top border,
// active tab marked by a 2px accent indicator (no pill, no glass blur). The
// gap above this bar still hosts a future mini-player without touching the tabs.
export function TabBar({ state, descriptors, navigation }: BottomTabBarProps) {
  const theme = useTheme();
  const insets = useSafeAreaInsets();

  const items = state.routes
    .flatMap((route, index) => {
      const descriptor = descriptors[route.key];
      const opts = descriptor?.options as { href?: string | null } | undefined;
      if (opts?.href === null || !(route.name in ICONS)) {
        return [];
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
