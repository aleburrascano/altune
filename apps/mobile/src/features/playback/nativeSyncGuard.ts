// Reconciles native -> store: while a queue load is priming the native player,
// the very first PlaybackActiveTrackChanged native fires is a transient index-0
// (adding tracks to an empty queue) before TrackPlayer.skip lands on the real
// target. We suppress that transient so the store cursor never flickers onto the
// wrong track.
//
// Loads are tracked per generation rather than in a single slot: overlapping
// loads (e.g. a reload racing a rapid-skip burst) each keep their own target, so
// one load can never clobber another's suppression, and a load that never sees
// its target event cannot leave a stale slot behind that wrongly suppresses a
// later, legitimate index-0 event (repeat-all wrap, skip-to-top) — the wedge.
// endNativeLoad is called from a finally in loadNativeQueue, so every load
// always releases its guard.

interface PendingLoad {
  readonly generation: number;
  readonly targetIndex: number;
}

let generationSeq = 0;
let pending: readonly PendingLoad[] = [];

export function beginNativeLoad(targetIndex: number): number {
  const generation = ++generationSeq;
  pending = [...pending, { generation, targetIndex }];
  return generation;
}

export function endNativeLoad(generation?: number): void {
  pending = generation == null ? [] : pending.filter((p) => p.generation !== generation);
}

export function shouldApplyActiveIndex(index: number): boolean {
  if (pending.length === 0) return true;

  // Ignore the priming index-0 event a non-zero-target load fires first.
  const isPrimingTransient = index === 0 && pending.some((p) => p.targetIndex !== 0);
  if (isPrimingTransient) return false;

  // Any real (non-priming) event means the in-flight loads have reached their
  // resting track; clear every pending target so later events are never poisoned.
  pending = [];
  return true;
}
