import { NetworkError } from '@shared/failure';

import { withAuthDeadline, withinAuthDeadline } from '@shared/auth';

const AUTH_CALL_TIMEOUT_MS = 15_000;

beforeEach(() => {
  jest.useFakeTimers();
});

afterEach(() => {
  jest.useRealTimers();
});

describe('withinAuthDeadline()', () => {
  it('rejects with NetworkError(timeout) once the deadline passes', async () => {
    const caught = withinAuthDeadline(new Promise<string>(() => {}), 'getSession', 'cid-1').catch(
      (error: unknown) => error,
    );

    jest.advanceTimersByTime(AUTH_CALL_TIMEOUT_MS);
    const error = await caught;

    expect(error).toBeInstanceOf(NetworkError);
    expect(error).toMatchObject({
      failure: 'timeout',
      correlationId: 'cid-1',
      message: 'getSession timed out after 15000ms',
    });
  });

  it('ignores a lookup that settles after the deadline', async () => {
    let settle: (value: string) => void = () => {};
    const work = new Promise<string>((resolve) => {
      settle = resolve;
    });
    const caught = withinAuthDeadline(work, 'getSession').catch((error: unknown) => error);

    jest.advanceTimersByTime(AUTH_CALL_TIMEOUT_MS);
    settle('late');

    expect(await caught).toBeInstanceOf(NetworkError);
  });

  it('clears its timer when the work succeeds early', async () => {
    const result = await withinAuthDeadline(Promise.resolve('ok'), 'getSession');

    expect(result).toBe('ok');
    expect(jest.getTimerCount()).toBe(0);
  });

  it('clears its timer and passes the error through when the work rejects', async () => {
    const boom = new Error('boom');

    await expect(withinAuthDeadline(Promise.reject(boom), 'getSession')).rejects.toBe(boom);
    expect(jest.getTimerCount()).toBe(0);
  });
});

describe('withAuthDeadline()', () => {
  const AUTH_ACTION_TIMEOUT_MS = 20_000;

  function never(): Promise<string> {
    return new Promise<string>(() => {});
  }

  it('resolves with the work and releases its deadline', async () => {
    await expect(withAuthDeadline(Promise.resolve('signed in'))).resolves.toBe('signed in');
    expect(jest.getTimerCount()).toBe(0);
  });

  it('passes the work error through and releases its deadline', async () => {
    const boom = new Error('boom');

    await expect(withAuthDeadline(Promise.reject(boom))).rejects.toBe(boom);
    expect(jest.getTimerCount()).toBe(0);
  });

  it('rejects with NetworkError(timeout) after 20 seconds, not before', async () => {
    let outcome: unknown = 'pending';
    void withAuthDeadline(never()).catch((error: unknown) => {
      outcome = error;
    });

    await jest.advanceTimersByTimeAsync(AUTH_ACTION_TIMEOUT_MS - 1);
    expect(outcome).toBe('pending');

    await jest.advanceTimersByTimeAsync(1);
    expect(outcome).toBeInstanceOf(NetworkError);
    expect(outcome).toMatchObject({
      failure: 'timeout',
      message: 'auth call exceeded the 20000ms timeout',
    });
  });

  it('takes a shorter deadline from the caller', async () => {
    const SHORT_MS = 500;
    const caught = withAuthDeadline(never(), SHORT_MS).catch((error: unknown) => error);

    await jest.advanceTimersByTimeAsync(SHORT_MS);

    expect(await caught).toMatchObject({ message: 'auth call exceeded the 500ms timeout' });
  });
});
