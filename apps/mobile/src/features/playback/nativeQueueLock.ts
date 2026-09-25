// Budget for one serialized native op, measured from when it starts running (not
// when it was queued). A queue op is a handful of bridge calls (reset, add, skip,
// play) that normally settle well under a second; this is generous enough for a
// slow device adding a large queue, yet short enough that a stalled bridge call
// frees skip/reorder/append instead of freezing them for the rest of the session.
export const NATIVE_QUEUE_OP_TIMEOUT_MS = 15_000;

export class NativeQueueTimeoutError extends Error {
  constructor(timeoutMs: number) {
    super(`Playback command timed out after ${timeoutMs / 1000}s`);
    this.name = 'NativeQueueTimeoutError';
  }
}

let chain: Promise<unknown> = Promise.resolve();

function armDeadline(): { expired: Promise<never>; clear: () => void } {
  let timer: ReturnType<typeof setTimeout> | undefined;
  const expired = new Promise<never>((_, reject) => {
    timer = setTimeout(
      () => reject(new NativeQueueTimeoutError(NATIVE_QUEUE_OP_TIMEOUT_MS)),
      NATIVE_QUEUE_OP_TIMEOUT_MS,
    );
  });
  return { expired, clear: () => clearTimeout(timer) };
}

function withDeadline<T>(op: () => Promise<T>): Promise<T> {
  const pending = new Promise<T>((resolve) => resolve(op()));
  pending.catch(() => undefined);
  const { expired, clear } = armDeadline();
  return Promise.race([pending, expired]).finally(clear);
}

export function withNativeQueue<T>(op: () => Promise<T>): Promise<T> {
  const start = () => withDeadline(op);
  const run = chain.then(start, start);
  chain = run.catch(() => undefined);
  return run;
}
