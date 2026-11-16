import { useCallback, useEffect, useState } from 'react';
import { AccessibilityInfo, Animated } from 'react-native';

export function useReduceMotion(): boolean {
  const [reduced, setReduced] = useState(false);

  useEffect(() => {
    let active = true;
    void AccessibilityInfo.isReduceMotionEnabled().then((value) => {
      if (active) {
        setReduced(value);
      }
    });
    const sub = AccessibilityInfo.addEventListener('reduceMotionChanged', setReduced);
    return () => {
      active = false;
      sub.remove();
    };
  }, []);

  return reduced;
}

const SPRING = { stiffness: 280, damping: 18, mass: 0.6, useNativeDriver: true };

export function usePressScale(pressedScale = 0.97) {
  const reduceMotion = useReduceMotion();
  const [scale] = useState(() => new Animated.Value(1));

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
