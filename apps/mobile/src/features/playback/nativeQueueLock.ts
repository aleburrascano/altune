// Budget for one serialized native op, measured from when it starts running (not
// when it was queued). A queue op is a handful of bridge calls (reset, add, skip,
// play) that normally settle well under a second; this is generous enough for a
// slow device adding a large queue, yet short enough that a stalled bridge call
// frees skip/reorder/append instead of freezing them for the rest of the session.
export const NATIVE_QUEUE_OP_TIMEOUT_MS = 15_000;

/**
 * Rejected to the caller whose native op outlived its budget. The op itself cannot
 * be cancelled and may still settle later; its late result is discarded.
 */
export class NativeQueueTimeoutError extends Error {
  constructor(timeoutMs: number) {
    super(`Playback command timed out after ${timeoutMs / 1000}s`);
    this.name = 'NativeQueueTimeoutError';
  }
}

let chain: Promise<unknown> = Promise.resolve();

function withDeadline<T>(op: () => Promise<T>): Promise<T> {
  let timer: ReturnType<typeof setTimeout> | undefined;
  const pending = new Promise<T>((resolve) => resolve(op()));
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

export function withNativeQueue<T>(op: () => Promise<T>): Promise<T> {
  const start = () => withDeadline(op);
  const run = chain.then(start, start);
  chain = run.catch(() => undefined);
  return run;
}
