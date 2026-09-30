const MAX_EXPONENT = 30;

export function clampedExponent(attempt: number, floor: number): number {
  return Math.min(Math.max(attempt, floor), MAX_EXPONENT);
}

export function equalJitterMs(
  baseMs: number,
  capMs: number,
  exponent: number,
  random: number,
): number {
  const ceiling = Math.min(capMs, baseMs * 2 ** exponent);
  return Math.round(ceiling / 2 + random * (ceiling / 2));
}
