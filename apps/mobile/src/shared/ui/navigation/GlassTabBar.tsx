import type { BottomTabBarProps } from '@react-navigation/bottom-tabs';
import { BlurView } from 'expo-blur';
import { Compass, Library as LibraryIcon } from 'lucide-react-native';
import type { ComponentType } from 'react';
import { Platform, Pressable, StyleSheet, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { Text } from '../primitives/Text';
import { radius, spacing } from '../theme/tokens';
import { useTheme } from '../theme/useTheme';

type IconComponent = ComponentType<{ size?: number; color?: string }>;

const ICONS: Record<string, IconComponent> = {
  discover: Compass,
  library: LibraryIcon,
};

// AIDEV-NOTE: ADR-0008 — floating glass bottom bar, rendered by the (tabs)
// navigator via its `tabBar` prop. iOS gets a real BlurView; Android falls back
// to a solid surface (BlurView is unreliable there). A future mini-player can
// mount in the gap ABOVE this bar without changing the tab logic.
export function GlassTabBar({ state, descriptors, navigation }: BottomTabBarProps) {
  const theme = useTheme();
  const insets = useSafeAreaInsets();

  const items = state.routes.map((route, index) => {
    const focused = state.index === index;
    const descriptor = descriptors[route.key];
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
        style={styles.tab}
      >
        <View
          style={[styles.iconPill, focused ? { backgroundColor: theme.color.accentTint } : null]}
        >
          <Icon size={22} color={color} />
        </View>
        <Text variant="caption" style={{ color, marginTop: 2 }}>
          {label}
        </Text>
      </Pressable>
    );
  });

  const bottomPad = insets.bottom > 0 ? insets.bottom : spacing.md;

  return (
    <View style={[styles.container, { paddingBottom: bottomPad }]}>
      {/* AIDEV-NOTE: future mini-player docks in the space above this bar. */}
      {Platform.OS === 'ios' ? (
        <BlurView
          tint="dark"
          intensity={40}
          style={[styles.bar, { borderColor: theme.color.border }]}
        >
          {items}
        </BlurView>
      ) : (
        <View
          style={[
            styles.bar,
            { backgroundColor: theme.color.surface2, borderColor: theme.color.border },
          ]}
        >
          {items}
        </View>
      )}
    </View>
  );
}

const styles = StyleSheet.create({
  container: {
    paddingHorizontal: spacing.lg,
    backgroundColor: 'transparent',
  },
  bar: {
    flexDirection: 'row',
    borderRadius: radius.xl,
    overflow: 'hidden',
    borderWidth: StyleSheet.hairlineWidth,
    paddingVertical: spacing.sm,
  },
  tab: {
    flex: 1,
    alignItems: 'center',
    justifyContent: 'center',
    paddingVertical: spacing.xs,
  },
  iconPill: {
    paddingHorizontal: spacing.lg,
    paddingVertical: 2,
    borderRadius: radius.full,
  },
});
