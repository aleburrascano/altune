// The maximum number of "Save all" writes allowed to be in flight at once.
// Without a cap, an album with many unowned tracks fires that many simultaneous
// POST /v1/tracks calls (each trailing three cache invalidations), swamping the
// device and the API in one tap.
export const SAVE_ALL_CONCURRENCY = 4;

// Runs `worker` over `items` with at most `limit` in flight at any moment.
// A single rejected worker does not abort the batch — siblings still run and the
// returned promise resolves once every item has settled.
export async function runBounded<T>(
  items: readonly T[],
  limit: number,
  worker: (item: T) => Promise<unknown>,
): Promise<void> {
  const queue = [...items];
  const runNext = async (): Promise<void> => {
    const item = queue.shift();
    if (item === undefined) return;
    await worker(item).catch(() => undefined);
    await runNext();
  };
  const lanes = Math.max(1, Math.min(limit, queue.length));
  await Promise.all(Array.from({ length: lanes }, () => runNext()));
}
