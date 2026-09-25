import { NetworkError } from '@shared/errors';

import { AUTH_CALL_TIMEOUT_MS, withinAuthDeadline } from '../authDeadline';

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
    expect(error).toMatchObject({ failure: 'timeout', correlationId: 'cid-1' });
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
