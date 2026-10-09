import * as Haptics from 'expo-haptics';

const IMPACT_STYLES = {
  light: Haptics.ImpactFeedbackStyle.Light,
  medium: Haptics.ImpactFeedbackStyle.Medium,
} as const;

export function tapFeedback(kind: 'light' | 'medium' | 'selection' = 'light'): void {
  if (kind === 'selection') {
    void Haptics.selectionAsync();
    return;
  }
  void Haptics.impactAsync(IMPACT_STYLES[kind]);
}
