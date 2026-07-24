import { AccessibilityInfo, Platform } from 'react-native';

export function announce(message: string): void {
  if (message.length === 0) return;
  if (Platform.OS === 'android') return;
  void AccessibilityInfo.isScreenReaderEnabled().then((enabled) => {
    if (enabled) AccessibilityInfo.announceForAccessibility(message);
  });
}
