import { useCallback, useRef } from 'react';
import { Animated } from 'react-native';

import { useReduceMotion } from './useReduceMotion';

const SPRING = { stiffness: 280, damping: 18, mass: 0.6, useNativeDriver: true };

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
