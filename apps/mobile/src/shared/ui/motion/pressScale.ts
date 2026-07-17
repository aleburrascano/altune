import { useCallback, useRef } from 'react';
import { Animated } from 'react-native';

import { useReduceMotion } from './useReduceMotion';

const SPRING = { stiffness: 280, damping: 18, mass: 0.6, useNativeDriver: true };

/** Subtle spring scale-down on press — the "tasteful/minimal" press feedback.
 * Uses RN's built-in Animated (no native worklets dep, works in Expo Go). Wire
 * onPressIn/onPressOut to a Pressable and apply `animatedStyle` to a wrapping
 * Animated.View (from 'react-native'). Honors the OS reduce-motion setting: the
 * scale stays flat, so the press is instant rather than sprung. */
export function usePressScale(pressedScale = 0.97) {
  const reduceMotion = useReduceMotion();
  const scaleRef = useRef<Animated.Value | null>(null);
  if (scaleRef.current === null) scaleRef.current = new Animated.Value(1);
  const scale = scaleRef.current;

  const onPressIn = useCallback(() => {
    if (reduceMotion) return;
    Animated.spring(scale, { toValue: pressedScale, ...SPRING }).start();
  }, [scale, pressedScale, reduceMotion]);

  const onPressOut = useCallback(() => {
    if (reduceMotion) return;
    Animated.spring(scale, { toValue: 1, ...SPRING }).start();
  }, [scale, reduceMotion]);

  const animatedStyle = { transform: [{ scale }] };

  return { onPressIn, onPressOut, animatedStyle };
}
