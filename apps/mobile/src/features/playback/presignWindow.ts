import { orderedQueueTracks, useQueueStore } from '@shared/playback/queueStore';
import type { PlaybackTrack } from '@shared/playback/types';

export const MAX_PRESIGN = 25;
// Re-presign the upcoming window once the queue has advanced to within this many
// tracks of the edge of the currently presigned block, so a long shuffle session
// never runs off the end of the initial MAX_PRESIGN signed URLs.
const PRESIGN_REFRESH_MARGIN = 5;

// Highest queue position (in playOrder space) whose native URL we have presigned.
// Tracked so the presign window can slide forward as playback advances instead of
// staying pinned to the first MAX_PRESIGN tracks loaded at queue start.
let presignedThrough = -1;

export function markPresignedFrom(startIndex: number, available: number): void {
  presignedThrough = startIndex + Math.min(MAX_PRESIGN, available) - 1;
}

// Slide the presign window forward as the queue advances. The native queue may
// hold hundreds of tracks but only MAX_PRESIGN of them carry a fresh signed URL;
// left alone, a long shuffle session eventually reaches unsigned tracks and
// stalls or repeats. When the active track nears the edge of the presigned block,
// re-presign the next window of upcoming tracks from the current position.
// `reorderUpcoming` is injected rather than imported so this module never depends
// on the TrackPlayer-facing load operations (which depend on it), avoiding a cycle.
export async function refreshUpcomingPresign(
  currentIndex: number,
  reorderUpcoming: (upcoming: readonly PlaybackTrack[]) => Promise<void>,
): Promise<void> {
  if (currentIndex < 0) return;
  if (presignedThrough - currentIndex > PRESIGN_REFRESH_MARGIN) return;
  const s = useQueueStore.getState();
  const upcoming = orderedQueueTracks(s).slice(currentIndex + 1);
  if (upcoming.length === 0) return;
  markPresignedFrom(currentIndex + 1, upcoming.length);
  await reorderUpcoming(upcoming);
}
