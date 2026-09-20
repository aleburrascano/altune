import {
  NATIVE_QUEUE_OP_TIMEOUT_MS,
  NativeQueueTimeoutError,
  withNativeQueue,
} from '../nativeQueueLock';

function deferred<T>(): {
  promise: Promise<T>;
  resolve: (v: T) => void;
  reject: (e: unknown) => void;
} {
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

  describe('when a native op never settles', () => {
    beforeEach(() => jest.useFakeTimers());
    afterEach(() => jest.useRealTimers());

    it('times the hung op out and still runs the operation queued behind it', async () => {
      const order: string[] = [];

      const hung = withNativeQueue(() => new Promise<void>(() => {}));
      const hungOutcome = hung.catch((err: unknown) => err);
      const next = withNativeQueue(async () => {
        order.push('next');
        return 'ran';
      });

      await jest.advanceTimersByTimeAsync(NATIVE_QUEUE_OP_TIMEOUT_MS - 1);
      expect(order).toEqual([]);

      await jest.advanceTimersByTimeAsync(1);

      const err = await hungOutcome;
      expect(err).toBeInstanceOf(NativeQueueTimeoutError);
      expect((err as Error).message).toMatch(/timed out/);
      await expect(next).resolves.toBe('ran');
      expect(order).toEqual(['next']);
    });

    it('starts each op budget when it begins running, not when it was queued', async () => {
      const gate = deferred<void>();
      const first = withNativeQueue(() => gate.promise);
      const second = withNativeQueue(
        () => new Promise<string>((resolve) => setTimeout(() => resolve('second'), 1_000)),
      );

      await jest.advanceTimersByTimeAsync(NATIVE_QUEUE_OP_TIMEOUT_MS - 500);
      gate.resolve();
      await first;

      await jest.advanceTimersByTimeAsync(1_000);
      await expect(second).resolves.toBe('second');
    });

    it('discards a late rejection from a timed-out op without an unhandled rejection', async () => {
      const late = deferred<void>();
      const hung = withNativeQueue(() => late.promise);
      const hungOutcome = hung.catch((err: unknown) => err);

      await jest.advanceTimersByTimeAsync(NATIVE_QUEUE_OP_TIMEOUT_MS);
      expect(await hungOutcome).toBeInstanceOf(NativeQueueTimeoutError);

      late.reject(new Error('bridge finally failed'));
      await expect(withNativeQueue(async () => 'after')).resolves.toBe('after');
    });

    it('clears the deadline once an op settles in time', async () => {
      await withNativeQueue(async () => 'quick');
      expect(jest.getTimerCount()).toBe(0);
    });
  });
});
