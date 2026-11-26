import { AccessibilityInfo } from 'react-native';

import type { ScreenReader } from '@shared/ui/view/a11y/createAnnounce';

export const screenReader: ScreenReader = {
  isEnabled: () => AccessibilityInfo.isScreenReaderEnabled(),
  speak: (message) => AccessibilityInfo.announceForAccessibility(message),
};
