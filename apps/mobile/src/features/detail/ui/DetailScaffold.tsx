import { useRef, useState, type ReactElement, type ReactNode } from 'react';
import { Animated, Pressable, StyleSheet, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { Image } from 'expo-image';
import { LinearGradient } from 'expo-linear-gradient';
import { ChevronLeft, MoreHorizontal } from 'lucide-react-native';

import { Text } from '@shared/ui/primitives/Text';
import { ContextMenu, type ContextMenuItem } from '@shared/ui/primitives/ContextMenu';
import { spacing, useTheme } from '@shared/ui/theme';

const BANNER_HEIGHT = 318;
const BAR_HEIGHT = 52;
const SCROLL_TAIL = 140;

function withAlpha(hex: string, alpha: number): string {
  const value = hex.replace('#', '');
  const r = parseInt(value.slice(0, 2), 16);
  const g = parseInt(value.slice(2, 4), 16);
  const b = parseInt(value.slice(4, 6), 16);
  return `rgba(${r},${g},${b},${alpha})`;
}

export type DetailChrome = {
  title: string;
  secondary?: ReactNode;
  artworkUrl: string | null;
  onBack: () => void;
};

export function DetailScaffold({
  title,
  secondary,
  artworkUrl,
  actions,
  facts,
  menuItems,
  onBack,
  children,
}: DetailChrome & {
  actions: ReactNode;
  facts?: ReactNode;
  menuItems?: ContextMenuItem[];
  children: ReactNode;
}): ReactElement {
  const theme = useTheme();
  const insets = useSafeAreaInsets();
  const [menuOpen, setMenuOpen] = useState(false);

  const scrollY = useRef(new Animated.Value(0)).current;
  const bannerHeight = BANNER_HEIGHT + insets.top;
  const chromeOpacity = scrollY.interpolate({
    inputRange: [bannerHeight - BAR_HEIGHT - insets.top - 40, bannerHeight - BAR_HEIGHT - insets.top],
    outputRange: [0, 1],
    extrapolate: 'clamp',
  });

  const hasMenu = menuItems != null && menuItems.length > 0;
  const canvas = theme.color.canvas;

  return (
    <View testID="detail-header" style={[styles.root, { backgroundColor: canvas }]}>
      <Animated.ScrollView
        showsVerticalScrollIndicator={false}
        scrollEventThrottle={16}
        contentContainerStyle={styles.content}
        onScroll={Animated.event([{ nativeEvent: { contentOffset: { y: scrollY } } }], {
          useNativeDriver: true,
        })}
      >
        <View style={[styles.banner, { height: bannerHeight }]}>
          <Image
            source={artworkUrl != null ? { uri: artworkUrl } : null}
            style={StyleSheet.absoluteFill}
            contentFit="cover"
            transition={200}
            accessibilityLabel={title}
          />
          <LinearGradient
            colors={[
              withAlpha(canvas, 0.55),
              withAlpha(canvas, 0.1),
              withAlpha(canvas, 0.34),
              withAlpha(canvas, 0.9),
              canvas,
            ]}
            locations={[0, 0.24, 0.54, 0.86, 1]}
            style={StyleSheet.absoluteFill}
            pointerEvents="none"
          />
          <View style={styles.bannerText}>
            <Text testID="detail-banner-title" variant="editorial" numberOfLines={2}>
              {title}
            </Text>
            {secondary != null ? <View style={styles.secondary}>{secondary}</View> : null}
          </View>
        </View>

        <View style={styles.body}>
          {actions}
          {facts}
          {children}
        </View>
      </Animated.ScrollView>

      <Animated.View
        pointerEvents="none"
        style={[
          styles.barFill,
          {
            height: insets.top + BAR_HEIGHT,
            backgroundColor: canvas,
            borderBottomColor: theme.color.border,
            opacity: chromeOpacity,
          },
        ]}
      />
      <View style={[styles.bar, { top: insets.top, height: BAR_HEIGHT }]}>
        <Pressable
          testID="detail-back"
          onPress={onBack}
          accessibilityRole="button"
          accessibilityLabel="Go back"
          hitSlop={8}
          style={({ pressed }) => [
            styles.glass,
            { backgroundColor: withAlpha(canvas, 0.42) },
            pressed ? styles.pressed : null,
          ]}
        >
          <ChevronLeft size={22} color={theme.color.textPrimary} />
        </Pressable>
        <Animated.View
          style={[styles.barTitle, { opacity: chromeOpacity }]}
          pointerEvents="none"
          accessibilityElementsHidden
          importantForAccessibility="no-hide-descendants"
        >
          <Text variant="bodyStrong" numberOfLines={1}>
            {title}
          </Text>
        </Animated.View>
        {hasMenu ? (
          <Pressable
            testID="detail-menu"
            onPress={() => setMenuOpen(true)}
            accessibilityRole="button"
            accessibilityLabel="More options"
            hitSlop={8}
            style={({ pressed }) => [
              styles.glass,
              { backgroundColor: withAlpha(canvas, 0.42) },
              pressed ? styles.pressed : null,
            ]}
          >
            <MoreHorizontal size={20} color={theme.color.textPrimary} />
          </Pressable>
        ) : (
          <View style={styles.glassSpacer} />
        )}
      </View>

      {hasMenu ? (
        <ContextMenu
          visible={menuOpen}
          items={menuItems}
          anchorTop={insets.top + BAR_HEIGHT}
          onClose={() => setMenuOpen(false)}
        />
      ) : null}
    </View>
  );
}

const GLASS = 40;

const styles = StyleSheet.create({
  root: { flex: 1 },
  content: { paddingBottom: SCROLL_TAIL },
  banner: { width: '100%', justifyContent: 'flex-end' },
  bannerText: { paddingHorizontal: spacing.lg, paddingBottom: spacing.lg },
  secondary: { marginTop: spacing.xs, alignSelf: 'flex-start' },
  body: { paddingHorizontal: spacing.lg },
  barFill: {
    position: 'absolute',
    top: 0,
    left: 0,
    right: 0,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
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
  pressed: { opacity: 0.6 },
});
