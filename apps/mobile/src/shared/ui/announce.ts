/**
 * Screen-reader announcements for state that changes without user input.
 *
 * `accessibilityLiveRegion` is Android-only; VoiceOver ignores it entirely. So
 * anything that must be heard on both platforms sets the prop *and* calls this,
 * which is the iOS half. Guarded on the screen reader actually running, so we
 * don't queue announcements nobody hears.
 */
import { AccessibilityInfo, Platform } from 'react-native';

export function announce(message: string): void {
  if (message.length === 0) return;
  if (Platform.OS === 'android') return; // accessibilityLiveRegion covers it
  void AccessibilityInfo.isScreenReaderEnabled().then((enabled) => {
    if (enabled) AccessibilityInfo.announceForAccessibility(message);
  });
}
