import { orderedQueueTracks, useQueueStore } from '@shared/playback/queueStore';
import type { PlaybackTrack } from '@shared/playback/types';

export const MAX_PRESIGN = 25;
// Re-presign the upcoming window once the queue has advanced to within this many
// tracks of the edge of the currently presigned block, so a long shuffle session
// never runs off the end of the initial MAX_PRESIGN signed URLs.
const PRESIGN_REFRESH_MARGIN = 5;

// How many tracks past the active one the native player is handed, bounding the bridge
// payload of a queue load and of every window slide below. It has to outrun the presign
// window rather than match it: the slide only fires once playback is within
// PRESIGN_REFRESH_MARGIN of the signed edge, so a burst of skips issued while that
// refresh is still in flight must still land on a track native already holds.
export const NATIVE_QUEUE_WINDOW = MAX_PRESIGN * 4;

// Highest queue position (in playOrder space) whose native URL we have presigned.
// Tracked so the presign window can slide forward as playback advances instead of
// staying pinned to the first MAX_PRESIGN tracks loaded at queue start.
let presignedThrough = -1;

export function markPresignedFrom(startIndex: number, available: number): void {
  presignedThrough = startIndex + Math.min(MAX_PRESIGN, available) - 1;
}

// Slide both windows forward as the queue advances. Only MAX_PRESIGN upcoming tracks
// carry a fresh signed URL and only NATIVE_QUEUE_WINDOW of them are in the native
// player at all; left alone, a long shuffle session eventually reaches unsigned tracks
// and stalls or repeats. When the active track nears the edge of the presigned block,
// hand the upcoming tracks back to `reorderUpcoming`, which re-presigns the next
// MAX_PRESIGN of them and rebuilds the native window from the current position.
// `reorderUpcoming` is injected rather than imported so this module never depends
// on the TrackPlayer-facing load operations (which depend on it), avoiding a cycle.
// It rejects to the caller: only the reorder installs the signed URLs, so marking the
// window before it resolves would pin `presignedThrough` to a block that never got
// them — unplayable tracks with no further refresh until playback caught up to them.
export async function refreshUpcomingPresign(
  currentIndex: number,
  reorderUpcoming: (upcoming: readonly PlaybackTrack[]) => Promise<void>,
): Promise<void> {
  if (currentIndex < 0) return;
  if (presignedThrough - currentIndex > PRESIGN_REFRESH_MARGIN) return;
  const s = useQueueStore.getState();
  const upcoming = orderedQueueTracks(s).slice(currentIndex + 1);
  if (upcoming.length === 0) return;
  await reorderUpcoming(upcoming);
  markPresignedFrom(currentIndex + 1, upcoming.length);
}
