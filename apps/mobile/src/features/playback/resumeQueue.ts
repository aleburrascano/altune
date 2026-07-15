// Pure resume-index resolution, extracted so it can be unit-tested without the
// TrackPlayer/network/store machinery of useQueueResume.
//
// The saved queue is persisted in play order with current_index pointing into
// that same library-only list, so savedTrackIds[savedCurrentIndex] is the id of
// the track that was playing. On restore, non-ready/missing tracks are dropped —
// which shifts positions — so we relocate the current track by id rather than
// trusting the raw index. Falls back to a clamped index for pre-fix saved rows
// (where the id can't be found) or when the current track itself was dropped.

export function currentTrackId(
  savedTrackIds: readonly string[],
  savedCurrentIndex: number,
): string {
  return savedTrackIds[savedCurrentIndex] ?? savedTrackIds[0] ?? '';
}

export function resolveResumeStartIndex(
  savedTrackIds: readonly string[],
  savedCurrentIndex: number,
  validTrackIds: readonly string[],
): number {
  if (validTrackIds.length === 0) return 0;
  const currentId = currentTrackId(savedTrackIds, savedCurrentIndex);
  const found = currentId ? validTrackIds.indexOf(currentId) : -1;
  if (found >= 0) return found;
  return Math.max(0, Math.min(savedCurrentIndex, validTrackIds.length - 1));
}

// Full-fidelity reconstruction: given the persisted natural (unshuffled) order and
// play order — both already filtered to ready/available ids — rebuild the store's
// playOrder (indices into naturalIds) and the currentIndex (a position in that
// playOrder). Defensive against the two lists diverging: play ids missing from
// naturalIds are skipped. currentId locates the current track's slot; if it's
// gone, currentIndex falls back to 0.
export function reconstructPlayOrder(
  naturalIds: readonly string[],
  playIds: readonly string[],
  currentId: string,
): { playOrder: number[]; currentIndex: number } {
  const naturalIndex = new Map<string, number>();
  naturalIds.forEach((id, i) => naturalIndex.set(id, i));

  const playOrder: number[] = [];
  let currentIndex = 0;
  for (const id of playIds) {
    const ni = naturalIndex.get(id);
    if (ni === undefined) continue; // in play order but dropped from natural — skip
    if (id === currentId) currentIndex = playOrder.length;
    playOrder.push(ni);
  }
  return { playOrder, currentIndex };
}
