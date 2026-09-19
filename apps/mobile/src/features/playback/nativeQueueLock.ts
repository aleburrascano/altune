// Budget for one serialized native op, measured from when it starts running (not
// when it was queued). A queue op is a handful of bridge calls (reset, add, skip,
// play) that normally settle well under a second; this is generous enough for a
// slow device adding a large queue, yet short enough that a stalled bridge call
// frees skip/reorder/append instead of freezing them for the rest of the session.
export const NATIVE_QUEUE_OP_TIMEOUT_MS = 15_000;

/**
 * Rejected to the caller whose native op outlived its budget. The op itself cannot be
 * cancelled and may still settle later; its late result is discarded, and the calls it
 * routes through its `NativeCallGuard` stop once the next op takes the lock.
 */
export class NativeQueueTimeoutError extends Error {
  constructor(timeoutMs: number) {
    super(`Playback command timed out after ${timeoutMs / 1000}s`);
    this.name = 'NativeQueueTimeoutError';
  }
}

/**
 * Rejected inside an op that lost the lock to the op queued behind it, so the loser stops
 * where its next bridge call would have been. Only reachable after a timeout: an op that
 * settles inside its budget still holds the lock when its last call runs.
 */
export class NativeQueueSupersededError extends Error {
  constructor() {
    super('Playback command was superseded');
    this.name = 'NativeQueueSupersededError';
  }
}

/**
 * Routes one bridge call for the op it was handed to. Calls made any other way are not
 * covered: a timed-out op can still reach TrackPlayer directly.
 */
export type NativeCallGuard = <T>(call: () => Promise<T>) => Promise<T>;

let chain: Promise<unknown> = Promise.resolve();

// Which op holds the run slot. The deadline frees `chain` without stopping the op it gave
// up on, so the lock alone cannot answer "is this call still wanted" — the generation can.
let runningOpGeneration = 0;

function callsWhileCurrent(generation: number): NativeCallGuard {
  return (call) =>
    generation === runningOpGeneration ? call() : Promise.reject(new NativeQueueSupersededError());
}

function withDeadline<T>(op: (ifCurrent: NativeCallGuard) => Promise<T>): Promise<T> {
  let timer: ReturnType<typeof setTimeout> | undefined;
  const generation = ++runningOpGeneration;
  const pending = new Promise<T>((resolve) => resolve(op(callsWhileCurrent(generation))));
  // A timed-out op that rejects later must not surface as an unhandled rejection.
  pending.catch(() => undefined);
  const deadline = new Promise<never>((_, reject) => {
    timer = setTimeout(
      () => reject(new NativeQueueTimeoutError(NATIVE_QUEUE_OP_TIMEOUT_MS)),
      NATIVE_QUEUE_OP_TIMEOUT_MS,
    );
  });
  return Promise.race([pending, deadline]).finally(() => clearTimeout(timer));
}

export function withNativeQueue<T>(op: (ifCurrent: NativeCallGuard) => Promise<T>): Promise<T> {
  const start = () => withDeadline(op);
  const run = chain.then(start, start);
  chain = run.catch(() => undefined);
  return run;
}
