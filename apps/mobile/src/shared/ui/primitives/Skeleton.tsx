import { useEffect, useRef } from 'react';
import { Animated } from 'react-native';
import type { DimensionValue, StyleProp, ViewStyle } from 'react-native';

import { useReduceMotion } from '../motion/useReduceMotion';
import { radius as radiusTokens } from '../theme/tokens';
import { useTheme } from '../theme/useTheme';

export type SkeletonProps = {
  width?: DimensionValue;
  height?: number;
  radius?: number;
  style?: StyleProp<ViewStyle>;
};

/** Shimmering placeholder block for loading states. Uses RN's built-in
 * Animated (no native worklets dep). Renders static when the OS reduce-motion
 * setting is on. */
export function Skeleton({
  width = '100%',
  height = 16,
  radius = radiusTokens.sm,
  style,
}: SkeletonProps) {
  const theme = useTheme();
  const reduceMotion = useReduceMotion();
  const opacity = useRef(new Animated.Value(0.5)).current;

  useEffect(() => {
    if (reduceMotion) {
      opacity.setValue(0.5);
      return;
    }
    const loop = Animated.loop(
      Animated.sequence([
        Animated.timing(opacity, { toValue: 1, duration: 800, useNativeDriver: true }),
        Animated.timing(opacity, { toValue: 0.5, duration: 800, useNativeDriver: true }),
      ]),
    );
    loop.start();
    return () => loop.stop();
  }, [reduceMotion, opacity]);

  return (
    <Animated.View
      accessibilityElementsHidden
      importantForAccessibility="no-hide-descendants"
      style={[
        { width, height, borderRadius: radius, backgroundColor: theme.color.surface2, opacity },
        style,
      ]}
    />
  );
}
