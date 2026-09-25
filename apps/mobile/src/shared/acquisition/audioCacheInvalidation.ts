import type { TrackId } from '@shared/api-client/ids';

type Invalidator = (trackId: TrackId) => void;

const invalidators = new Set<Invalidator>();

export function registerAudioCacheInvalidator(fn: Invalidator): () => void {
  invalidators.add(fn);
  return () => {
    invalidators.delete(fn);
  };
}

export function invalidateAudioCaches(trackId: TrackId): void {
  for (const invalidate of invalidators) {
    try {
      invalidate(trackId);
    } catch (error) {
      console.warn(`[acquisition] an audio-cache invalidator failed for track ${trackId}`, error);
    }
  }
}

export function _resetAudioCacheInvalidatorsForTest(): void {
  invalidators.clear();
}
