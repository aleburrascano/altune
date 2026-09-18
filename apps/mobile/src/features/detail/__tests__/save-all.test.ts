import { runBounded, SAVE_ALL_CONCURRENCY } from '../save-all';

type Deferred = { resolve: () => void; reject: (e: unknown) => void };

// A worker double that records how many calls are in flight at once and only
// settles when the test releases each one — so concurrency is observable.
function tracker() {
  let active = 0;
  let maxConcurrent = 0;
  const started: number[] = [];
  const pending: Deferred[] = [];
  const worker = (n: number): Promise<void> => {
    active += 1;
    maxConcurrent = Math.max(maxConcurrent, active);
    started.push(n);
    return new Promise<void>((resolve, reject) => {
      pending.push({
        resolve: () => {
          active -= 1;
          resolve();
        },
        reject: (e) => {
          active -= 1;
          reject(e);
        },
      });
    });
  };
  return { worker, started, pending, get maxConcurrent() {
    return maxConcurrent;
  } };
}

async function flush(): Promise<void> {
  await Promise.resolve();
  await Promise.resolve();
}

describe('runBounded', () => {
  it('never runs more than the limit of workers at once, yet processes every item', async () => {
    const t = tracker();
    const items = Array.from({ length: 20 }, (_, i) => i);

    const done = runBounded(items, 4, t.worker);

    // The first wave fills exactly the lane budget, not all 20.
    expect(t.started).toHaveLength(4);
    for (let guard = 0; guard < 50 && t.pending.length > 0; guard += 1) {
      t.pending.splice(0).forEach((d) => d.resolve());
      await flush();
    }
    await done;

    expect(t.started).toHaveLength(20);
    expect(t.maxConcurrent).toBe(4);
  });

  it('lets a rejected worker not abort its siblings, and reports which item failed', async () => {
    const t = tracker();
    const items = [0, 1, 2];

    const done = runBounded(items, 3, t.worker);
    expect(t.pending).toHaveLength(3);

    t.pending[1]!.reject(new Error('save failed'));
    t.pending[0]!.resolve();
    t.pending[2]!.resolve();
    await flush();

    // Which items got through is the difference between "retry the batch" and
    // "retry the one that failed", so the outcome names them rather than dropping them.
    await expect(done).resolves.toEqual({ succeeded: [0, 2], failed: [1] });
    expect(t.started).toEqual([0, 1, 2]);
  });

  it('caps at the item count when there are fewer items than the limit', async () => {
    const t = tracker();

    const done = runBounded([0, 1], SAVE_ALL_CONCURRENCY, t.worker);
    expect(t.started).toHaveLength(2);

    t.pending.splice(0).forEach((d) => d.resolve());
    await flush();
    await done;

    expect(t.maxConcurrent).toBe(2);
  });
});
