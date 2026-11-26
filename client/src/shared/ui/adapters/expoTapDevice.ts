import * as Haptics from 'expo-haptics';

import type { TapDevice } from '@shared/ui/ports/tapFeedback';

const IMPACT_STYLES = {
  light: Haptics.ImpactFeedbackStyle.Light,
  medium: Haptics.ImpactFeedbackStyle.Medium,
} as const;

export const expoTapDevice: TapDevice = {
  impact: (style) => {
    void Haptics.impactAsync(IMPACT_STYLES[style]);
  },
  selection: () => {
    void Haptics.selectionAsync();
  },
};
