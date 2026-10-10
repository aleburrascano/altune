import { runBounded } from '@shared/runBounded';

const limit = 4;

describe('runBounded', () => {
  type Deferred = { resolve: () => void; reject: (e: unknown) => void };

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
    return {
      worker,
      started,
      pending,
      get maxConcurrent() {
        return maxConcurrent;
      },
    };
  }

  async function flush(): Promise<void> {
    await Promise.resolve();
    await Promise.resolve();
  }

  it('never runs more than the limit of workers at once, yet processes every item', async () => {
    const t = tracker();
    const items = Array.from({ length: 20 }, (_, i) => i);

    const done = runBounded(items, limit, t.worker);

    expect(t.started).toHaveLength(limit);
    for (let guard = 0; guard < 50 && t.pending.length > 0; guard += 1) {
      t.pending.splice(0).forEach((d) => d.resolve());
      await flush();
    }
    await done;

    expect(t.started).toHaveLength(20);
    expect(t.maxConcurrent).toBe(limit);
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

    await expect(done).resolves.toEqual({ succeeded: [0, 2], failed: [1] });
    expect(t.started).toEqual([0, 1, 2]);
  });

  it('caps at the item count when there are fewer items than the limit', async () => {
    const t = tracker();

    const done = runBounded([0, 1], limit, t.worker);
    expect(t.started).toHaveLength(2);

    t.pending.splice(0).forEach((d) => d.resolve());
    await flush();
    await done;

    expect(t.maxConcurrent).toBe(2);
  });

  it('resolves empty and never calls the worker for 0 items', async () => {
    const t = tracker();

    await expect(runBounded([], limit, t.worker)).resolves.toEqual({ succeeded: [], failed: [] });

    expect(t.started).toEqual([]);
  });

  it('runs the worker exactly once for 1 item with limit 4', async () => {
    const t = tracker();

    const done = runBounded([0], limit, t.worker);
    t.pending.splice(0).forEach((d) => d.resolve());
    await flush();
    await done;

    expect(t.started).toEqual([0]);
  });

  it('stops taking items once shouldContinue turns false', async () => {
    const t = tracker();
    let calls = 0;
    function shouldContinue(): boolean {
      calls += 1;
      return calls === 1;
    }
    const items = [0, 1, 2];

    const done = runBounded(items, 1, t.worker, shouldContinue);
    t.pending.splice(0).forEach((d) => d.resolve());
    await flush();

    await expect(done).resolves.toEqual({ succeeded: [0], failed: [] });
    expect(t.started).toEqual([0]);
  });
});
