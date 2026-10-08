import { orderedQueueTracks, useQueueStore } from '@shared/playback/queueStore';
import type { PlaybackTrack } from '@shared/playback/types';

export const MAX_PRESIGN = 25;
const PRESIGN_REFRESH_MARGIN = 5;

export const NATIVE_QUEUE_WINDOW = MAX_PRESIGN * 4;

let presignedThrough = -1;

export function markPresignedFrom(startIndex: number, available: number): void {
  presignedThrough = startIndex + Math.min(MAX_PRESIGN, available) - 1;
}

export async function extendPresignWindow(
  currentIndex: number,
  reorderUpcoming: (upcoming: readonly PlaybackTrack[]) => Promise<boolean>,
): Promise<void> {
  if (currentIndex < 0 || presignedThrough - currentIndex > PRESIGN_REFRESH_MARGIN) return;
  const upcoming = orderedQueueTracks(useQueueStore.getState()).slice(currentIndex + 1);
  if (upcoming.length === 0) return;
  if (!(await reorderUpcoming(upcoming))) return;
  markPresignedFrom(currentIndex + 1, upcoming.length);
}
