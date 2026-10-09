export const RESTART_THRESHOLD_MS = 3_000;

export function shouldRestartOnPrevious(positionMs: number): boolean {
  return positionMs > RESTART_THRESHOLD_MS;
}
