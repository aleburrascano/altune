import { withNativeQueue } from '../nativeQueueLock';

function deferred<T>(): { promise: Promise<T>; resolve: (v: T) => void; reject: (e: unknown) => void } {
  let resolve!: (v: T) => void;
  let reject!: (e: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

const flush = (): Promise<void> => new Promise((r) => setImmediate(r));

describe('withNativeQueue — serialising native queue operations', () => {
  it('holds the second operation until the first one settles', async () => {
    const gate = deferred<void>();
    const order: string[] = [];

    const first = withNativeQueue(async () => {
      order.push('first-start');
      await gate.promise;
      order.push('first-end');
    });
    const second = withNativeQueue(async () => {
      order.push('second-start');
    });

    await flush();
    expect(order).toEqual(['first-start']);

    gate.resolve();
    await Promise.all([first, second]);

    expect(order).toEqual(['first-start', 'first-end', 'second-start']);
  });

  it('runs the next operation even after the previous one rejects', async () => {
    const order: string[] = [];

    const failing = withNativeQueue(async () => {
      order.push('failing');
      throw new Error('native reset failed');
    });

    await expect(failing).rejects.toThrow('native reset failed');

    await withNativeQueue(async () => {
      order.push('after');
    });

    expect(order).toEqual(['failing', 'after']);
  });

  it('keeps a rejecting operation before the one queued behind it', async () => {
    const order: string[] = [];

    const failing = withNativeQueue(async () => {
      order.push('failing');
      throw new Error('boom');
    });
    const next = withNativeQueue(async () => {
      order.push('next');
    });

    await Promise.allSettled([failing, next]);

    expect(order).toEqual(['failing', 'next']);
  });

  it('resolves each caller with its own operation result', async () => {
    const a = await withNativeQueue(async () => 'a');
    const b = await withNativeQueue(async () => 'b');

    expect(a).toBe('a');
    expect(b).toBe('b');
  });
});
