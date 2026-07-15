// AIDEV-NOTE: Guards the store from the spurious active-track transient that
// TrackPlayer.add emits when priming a queue. add() makes native index 0 the
// active track (firing PlaybackActiveTrackChanged(0)) before skip(startIndex)
// moves to the real one — without this guard the store flips to track[0] for a
// beat, flashing the wrong song on relaunch/queue-start.
//
// While a native load is in flight we drop only the index-0 priming transient.
// Any other index is a real position — the target, or a transition that raced
// past it (a next press landing before skip(target) does) — so the guard
// releases and lets native stay authoritative. Releasing on the first real index
// rather than only on an exact target match is what keeps a missed target from
// pinning the guard forever and silently dropping every later transition.
let pendingTargetIndex: number | null = null;

export function beginNativeLoad(targetIndex: number): void {
  pendingTargetIndex = targetIndex;
}

export function endNativeLoad(): void {
  pendingTargetIndex = null;
}

// Returns whether a native active-track index should be applied to the store.
// Normal operation (no load pending) always applies. During a load, the index-0
// transient is dropped — unless 0 IS the target, where it's the real position.
export function shouldApplyActiveIndex(index: number): boolean {
  if (pendingTargetIndex === null) return true;
  if (index === 0 && pendingTargetIndex !== 0) return false;
  pendingTargetIndex = null;
  return true;
}
