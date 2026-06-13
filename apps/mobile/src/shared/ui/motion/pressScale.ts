import { useCallback, useRef } from 'react';
import { Animated } from 'react-native';

const SPRING = { stiffness: 280, damping: 18, mass: 0.6, useNativeDriver: true };

/** Subtle spring scale-down on press — the "tasteful/minimal" press feedback.
 * Uses RN's built-in Animated (no native worklets dep, works in Expo Go). Wire
 * onPressIn/onPressOut to a Pressable and apply `animatedStyle` to a wrapping
 * Animated.View (from 'react-native'). */
export function usePressScale(pressedScale = 0.97) {
  const scaleRef = useRef<Animated.Value | null>(null);
  if (scaleRef.current === null) scaleRef.current = new Animated.Value(1);
  const scale = scaleRef.current;

  const onPressIn = useCallback(() => {
    Animated.spring(scale, { toValue: pressedScale, ...SPRING }).start();
  }, [scale, pressedScale]);

  const onPressOut = useCallback(() => {
    Animated.spring(scale, { toValue: 1, ...SPRING }).start();
  }, [scale]);

  const animatedStyle = { transform: [{ scale }] };

  return { onPressIn, onPressOut, animatedStyle };
}
