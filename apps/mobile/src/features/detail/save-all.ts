// The maximum number of "Save all" writes allowed to be in flight at once.
// Without a cap, an album with many unowned tracks fires that many simultaneous
// POST /v1/tracks calls (each trailing three cache invalidations), swamping the
// device and the API in one tap.
export const SAVE_ALL_CONCURRENCY = 4;

// Which items the batch wrote and which it did not. A batch that only reports
// "finished" cannot tell a clean run from a partial failure, so a retry re-submits
// the items that already succeeded (#1658).
export type BatchOutcome<T> = { succeeded: readonly T[]; failed: readonly T[] };

function laneCount(limit: number, itemCount: number): number {
  return Math.max(1, Math.min(limit, itemCount));
}

// Runs `worker` over `items` with at most `limit` in flight at any moment.
// A single rejected worker does not abort the batch — siblings still run, and every
// item's outcome is reported once all of them have settled.
export async function runBounded<T>(
  items: readonly T[],
  limit: number,
  worker: (item: T) => Promise<unknown>,
): Promise<BatchOutcome<T>> {
  const queue = [...items];
  const succeeded: T[] = [];
  const failed: T[] = [];
  const drain = async (): Promise<void> => {
    const item = queue.shift();
    if (item === undefined) return;
    await worker(item).then(
      () => void succeeded.push(item),
      () => void failed.push(item),
    );
    await drain();
  };
  await Promise.all(Array.from({ length: laneCount(limit, queue.length) }, () => drain()));
  return { succeeded, failed };
}
