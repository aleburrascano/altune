const FAST_START_MS = 2000;

export function playStartSpeed(startMs: number): 'fast' | 'slow' {
  return startMs <= FAST_START_MS ? 'fast' : 'slow';
}
