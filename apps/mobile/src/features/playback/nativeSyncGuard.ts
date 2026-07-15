// AIDEV-NOTE: Guards the store from the spurious active-track transient that
// TrackPlayer.add emits when priming a queue. add() makes native index 0 the
// active track (firing PlaybackActiveTrackChanged(0)) before skip(startIndex)
// moves to the real one — without this guard the store flips to track[0] for a
// beat, flashing the wrong song on relaunch/queue-start.
//
// While a native load is in flight we pin the *target* index: the service
// listener applies only the event matching the target (or any event once no load
// is pending) and drops the intermediate index-0. shouldApplyActiveIndex is
// self-clearing on the target, and endNativeLoad is a safety net so a load that
// never reaches its target can't leave the guard stuck.
let pendingTargetIndex: number | null = null;

export function beginNativeLoad(targetIndex: number): void {
  pendingTargetIndex = targetIndex;
}

export function endNativeLoad(): void {
  pendingTargetIndex = null;
}

// Returns whether a native active-track index should be applied to the store.
// Normal operation (no load pending) always applies. During a load, only the
// target index applies — and doing so ends the load window.
export function shouldApplyActiveIndex(index: number): boolean {
  if (pendingTargetIndex === null) return true;
  if (index === pendingTargetIndex) {
    pendingTargetIndex = null;
    return true;
  }
  return false;
}
