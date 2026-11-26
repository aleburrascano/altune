import { startDeadline, withDeadline } from '@shared/timing';

const DEADLINE_MS = 1000;
const JUST_BEFORE_DEADLINE_MS = DEADLINE_MS - 1;
const LONG_AFTER_DEADLINE_MS = 5000;

describe('withDeadline', () => {
  beforeEach(() => {
    jest.useFakeTimers();
  });

  afterEach(() => {
    jest.useRealTimers();
  });

  function expiredError(): Error {
    return new Error('too slow');
  }

  interface ListeningSignal {
    signal: AbortSignal;
    abortListeners: () => number;
  }

  function listeningSignal(): ListeningSignal {
    const listening: unknown[] = [];
    const signal = Object.assign(new AbortController().signal, {
      addEventListener(type: string, listener: unknown): void {
        if (type === 'abort') listening.push(listener);
      },
      removeEventListener(type: string, listener: unknown): void {
        const at = listening.indexOf(listener);
        if (type === 'abort' && at >= 0) listening.splice(at, 1);
      },
    });
    return { signal, abortListeners: () => listening.length };
  }

  function neverSettles<T>(): Promise<T> {
    return new Promise<T>(() => undefined);
  }

  it('resolves with the work result and leaves no timer running', async () => {
    const result = await withDeadline(Promise.resolve('done'), DEADLINE_MS, {
      onExpire: expiredError,
    });

    expect(result).toBe('done');
    expect(jest.getTimerCount()).toBe(0);
  });

  it('rejects with the error onExpire builds once ms elapses, and not a tick before', async () => {
    const outcome = jest.fn();
    withDeadline(neverSettles<string>(), DEADLINE_MS, { onExpire: expiredError }).catch(outcome);

    await jest.advanceTimersByTimeAsync(JUST_BEFORE_DEADLINE_MS);
    expect(outcome).not.toHaveBeenCalled();

    await jest.advanceTimersByTimeAsync(1);
    expect(outcome).toHaveBeenCalledWith(expect.objectContaining({ message: 'too slow' }));
  });

  it('runs the onExpire side effect once, when the deadline passes', async () => {
    const onExpire = jest.fn(expiredError);
    withDeadline(neverSettles<void>(), DEADLINE_MS, { onExpire }).catch(() => undefined);

    await jest.advanceTimersByTimeAsync(LONG_AFTER_DEADLINE_MS);

    expect(onExpire).toHaveBeenCalledTimes(1);
  });

  it("passes the work's own rejection through before the deadline, without calling onLate", async () => {
    const onLate = jest.fn();
    const failure = new Error('work failed');

    await expect(
      withDeadline(Promise.reject(failure), DEADLINE_MS, { onExpire: expiredError, onLate }),
    ).rejects.toBe(failure);

    expect(onLate).not.toHaveBeenCalled();
    expect(jest.getTimerCount()).toBe(0);
  });

  it('hands a failure after the deadline to onLate instead of leaving it unhandled', async () => {
    const onLate = jest.fn();
    const failure = new Error('failed after the deadline');
    const { promise: work, reject } = Promise.withResolvers<never>();
    const outcome = withDeadline(work, DEADLINE_MS, { onExpire: expiredError, onLate });
    outcome.catch(() => undefined);

    await jest.advanceTimersByTimeAsync(DEADLINE_MS);
    reject(failure);
    await jest.advanceTimersByTimeAsync(0);

    await expect(outcome).rejects.toThrow('too slow');
    expect(onLate).toHaveBeenCalledWith(failure);
  });

  it('ignores a failure after the deadline when no onLate is given', async () => {
    const { promise: work, reject } = Promise.withResolvers<never>();
    const outcome = withDeadline(work, DEADLINE_MS, { onExpire: expiredError });
    outcome.catch(() => undefined);

    await jest.advanceTimersByTimeAsync(DEADLINE_MS);
    reject(new Error('late'));
    await jest.advanceTimersByTimeAsync(0);

    await expect(outcome).rejects.toThrow('too slow');
    expect(jest.getTimerCount()).toBe(0);
  });

  it('leaves no timer running once the signal aborts, and the work decides the outcome', async () => {
    const stop = new AbortController();
    const { promise: work, resolve } = Promise.withResolvers<string>();
    const outcome = withDeadline(work, DEADLINE_MS, {
      onExpire: expiredError,
      signal: stop.signal,
    });

    stop.abort();
    expect(jest.getTimerCount()).toBe(0);
    await jest.advanceTimersByTimeAsync(LONG_AFTER_DEADLINE_MS);
    resolve('finished anyway');

    await expect(outcome).resolves.toBe('finished anyway');
  });

  it('listens for the signal only while the work is pending', async () => {
    const { signal, abortListeners } = listeningSignal();
    const { promise: work, resolve } = Promise.withResolvers<string>();
    const outcome = withDeadline(work, DEADLINE_MS, { onExpire: expiredError, signal });
    expect(abortListeners()).toBe(1);

    resolve('done');
    await outcome;

    expect(abortListeners()).toBe(0);
  });

  it('starts no timer when the signal is already aborted', () => {
    const stop = new AbortController();
    stop.abort();

    withDeadline(neverSettles<void>(), DEADLINE_MS, {
      onExpire: expiredError,
      signal: stop.signal,
    });

    expect(jest.getTimerCount()).toBe(0);
  });

  it('ignores a success after the deadline', async () => {
    const { promise: work, resolve } = Promise.withResolvers<string>();
    const outcome = withDeadline(work, DEADLINE_MS, { onExpire: expiredError });
    outcome.catch(() => undefined);

    await jest.advanceTimersByTimeAsync(DEADLINE_MS);
    resolve('too late');

    await expect(outcome).rejects.toThrow('too slow');
  });
});

describe('startDeadline', () => {
  beforeEach(() => {
    jest.useFakeTimers();
  });

  afterEach(() => {
    jest.useRealTimers();
  });

  describe('the timer', () => {
    it('is still not expired one tick before ms elapses, and expires at exactly ms', () => {
      const deadline = startDeadline(undefined, 1000);

      jest.advanceTimersByTime(999);
      expect(deadline.expired()).toBe(false);
      expect(deadline.signal.aborted).toBe(false);

      jest.advanceTimersByTime(1);
      expect(deadline.expired()).toBe(true);
      expect(deadline.signal.aborted).toBe(true);
    });
  });

  describe('no external signal (the majority path: most typed functions pass none)', () => {
    it('never throws wiring an external signal, cancelled() stays false, and the timer still fires', () => {
      expect(() => startDeadline(undefined, 1000)).not.toThrow();

      const deadline = startDeadline(undefined, 1000);
      expect(deadline.cancelled()).toBe(false);

      jest.advanceTimersByTime(1000);
      expect(deadline.expired()).toBe(true);
    });

    it('release() is safe to call with nothing to detach', () => {
      const deadline = startDeadline(undefined, 1000);
      expect(() => deadline.release()).not.toThrow();
    });
  });

  describe('an external signal already aborted before startDeadline is called', () => {
    it('aborts the internal signal immediately, without waiting for an "abort" event that will never fire', () => {
      const external = new AbortController();
      external.abort();

      const deadline = startDeadline(external.signal, 1000);

      expect(deadline.signal.aborted).toBe(true);
      expect(deadline.cancelled()).toBe(true);
      expect(deadline.expired()).toBe(false);
    });
  });

  describe('an external signal that aborts after startDeadline is called', () => {
    it('relays the abort through addEventListener', () => {
      const external = new AbortController();
      const deadline = startDeadline(external.signal, 1000);

      expect(deadline.signal.aborted).toBe(false);

      external.abort();

      expect(deadline.signal.aborted).toBe(true);
      expect(deadline.cancelled()).toBe(true);
      expect(deadline.expired()).toBe(false);
    });
  });

  describe('cancelled() and expired() as independent observers', () => {
    it('reports expired without cancelled when only the timer fires', () => {
      const external = new AbortController();
      const deadline = startDeadline(external.signal, 1000);

      jest.advanceTimersByTime(1000);

      expect(deadline.expired()).toBe(true);
      expect(deadline.cancelled()).toBe(false);
    });

    it('reports cancelled without expired when only the caller aborts', () => {
      const external = new AbortController();
      const deadline = startDeadline(external.signal, 1000);

      external.abort();

      expect(deadline.cancelled()).toBe(true);
      expect(deadline.expired()).toBe(false);
    });
  });

  describe('release()', () => {
    it('clears the pending timer so it never fires', () => {
      const deadline = startDeadline(undefined, 1000);

      deadline.release();
      jest.advanceTimersByTime(1000);

      expect(deadline.expired()).toBe(false);
      expect(deadline.signal.aborted).toBe(false);
    });

    it('detaches the abort listener so a later external abort has no effect', () => {
      const external = new AbortController();
      const deadline = startDeadline(external.signal, 1000);

      deadline.release();
      external.abort();

      expect(deadline.signal.aborted).toBe(false);
    });

    it('still clears the timer and detaches the listener when release() is reached via a finally after a throw', () => {
      const external = new AbortController();
      const deadline = startDeadline(external.signal, 1000);

      expect(() => {
        try {
          throw new Error('caller threw before release');
        } finally {
          deadline.release();
        }
      }).toThrow('caller threw before release');

      jest.advanceTimersByTime(1000);
      external.abort();
      expect(deadline.signal.aborted).toBe(false);
    });
  });
});
