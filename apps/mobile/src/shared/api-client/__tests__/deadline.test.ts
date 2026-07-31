import { startDeadline } from '../deadline';

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
