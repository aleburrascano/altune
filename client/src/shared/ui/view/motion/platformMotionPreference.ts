import { AccessibilityInfo } from 'react-native';

import type { MotionPreference } from '@shared/ui/ports/motionPreference';

export const platformMotionPreference: MotionPreference = {
  isReduced: () => AccessibilityInfo.isReduceMotionEnabled(),
  onChange: (listener) => {
    const subscription = AccessibilityInfo.addEventListener('reduceMotionChanged', listener);
    return () => subscription.remove();
  },
};
