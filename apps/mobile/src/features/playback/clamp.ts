// Bounds are applied low last, so a `max` below `min` yields `min`: an index clamp against
// an empty list (max = length - 1 = -1) lands on 0, not on -1.
export function clamp(value: number, min: number, max: number): number {
  return Math.max(min, Math.min(value, max));
}
