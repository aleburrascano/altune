type Invalidator = (trackId: string) => void;

const invalidators = new Set<Invalidator>();

export function registerAudioCacheInvalidator(fn: Invalidator): () => void {
  invalidators.add(fn);
  return () => {
    invalidators.delete(fn);
  };
}

export function invalidateAudioCaches(trackId: string): void {
  for (const invalidate of invalidators) {
    try {
      invalidate(trackId);
    } catch {}
  }
}

export function _resetAudioCacheInvalidatorsForTest(): void {
  invalidators.clear();
}
