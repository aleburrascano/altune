import * as Haptics from 'expo-haptics';

import { isWebPlatform } from '@shared/device';
import type { TapDevice } from '@shared/ui/ports/tapFeedback';

export type HapticsLib<Style = unknown> = {
  impactAsync: (style: Style) => Promise<void>;
  selectionAsync: () => Promise<void>;
  ImpactFeedbackStyle: { Light: Style; Medium: Style };
};

export function createExpoTapDevice<Style>(
  haptics: HapticsLib<Style>,
  isWeb: () => boolean,
): TapDevice {
  const impactStyles = {
    light: haptics.ImpactFeedbackStyle.Light,
    medium: haptics.ImpactFeedbackStyle.Medium,
  } as const;
  return {
    impact: (style) => {
      if (isWeb()) return;
      void haptics.impactAsync(impactStyles[style]);
    },
    selection: () => {
      if (isWeb()) return;
      void haptics.selectionAsync();
    },
  };
}

export const expoTapDevice = createExpoTapDevice(Haptics, isWebPlatform);
