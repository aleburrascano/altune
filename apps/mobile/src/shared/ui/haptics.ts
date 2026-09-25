import * as Haptics from 'expo-haptics';
import { Platform } from 'react-native';

const IMPACT_STYLES = {
  light: Haptics.ImpactFeedbackStyle.Light,
  medium: Haptics.ImpactFeedbackStyle.Medium,
} as const;

export function tapFeedback(kind: 'light' | 'medium' | 'selection' = 'light'): void {
  if (Platform.OS === 'web') return;
  if (kind === 'selection') {
    void Haptics.selectionAsync();
    return;
  }
  void Haptics.impactAsync(IMPACT_STYLES[kind]);
}
