import type { TapDevice, TapFeedback } from '@shared/ui/ports/tapFeedback';

export function createTapFeedback(device: TapDevice): TapFeedback {
  return (kind = 'light') => {
    if (kind === 'selection') {
      device.selection();
      return;
    }
    device.impact(kind);
  };
}
