import type { MotionPreference } from '@shared/ui';

export function fixedMotion(reduced: boolean): MotionPreference {
  return {
    isReduced: () => Promise.resolve(reduced),
    onChange: () => () => undefined,
  };
}
