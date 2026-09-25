import { AUTH_ACTION_TIMEOUT_MS } from '../authDeadline';
import { completeAuthIntent, _resetConsumedCredentialForTest } from '../completeAuthIntent';
import { parseAuthLink } from '../parseAuthLink';

const auth = {
  exchangeCodeForSession: jest.fn(),
  verifyOtp: jest.fn(),
};

const router = { replace: jest.fn() };

function neverSettles<T>(): Promise<T> {
  return new Promise<T>(() => {});
}

beforeEach(() => {
  jest.useFakeTimers();
  auth.exchangeCodeForSession.mockReset().mockResolvedValue({ data: {}, error: null });
  auth.verifyOtp.mockReset().mockResolvedValue({ data: {}, error: null });
  router.replace.mockClear();
  _resetConsumedCredentialForTest();
});

afterEach(() => {
  jest.useRealTimers();
});

describe('completeAuthIntent: an exchange that never settles (#2468)', () => {
  it('keeps the claim on a stalled code exchange until the auth deadline', async () => {
    auth.exchangeCodeForSession.mockReturnValueOnce(neverSettles());
    const url = 'altune://auth/callback?code=stalled-before-deadline';

    void completeAuthIntent(parseAuthLink(url), router, auth).catch(() => undefined);
    await jest.advanceTimersByTimeAsync(AUTH_ACTION_TIMEOUT_MS - 1);
    void completeAuthIntent(parseAuthLink(url), router, auth).catch(() => undefined);

    expect(auth.exchangeCodeForSession).toHaveBeenCalledTimes(1);
  });

  it('abandons a stalled code exchange at the auth deadline and re-spends the retapped link', async () => {
    auth.exchangeCodeForSession.mockReturnValueOnce(neverSettles());
    const url = 'altune://auth/callback?code=stalled-then-retapped';

    const stalledRejection = expect(
      completeAuthIntent(parseAuthLink(url), router, auth),
    ).rejects.toMatchObject({ name: 'NetworkError', failure: 'timeout' });
    await jest.advanceTimersByTimeAsync(AUTH_ACTION_TIMEOUT_MS);
    const retry = completeAuthIntent(parseAuthLink(url), router, auth);

    expect(auth.exchangeCodeForSession).toHaveBeenCalledTimes(2);
    await expect(retry).resolves.toEqual({ kind: 'success' });
    await stalledRejection;
  });

  it('abandons a stalled recovery verification at the auth deadline and re-verifies the retapped link', async () => {
    auth.verifyOtp
      .mockReturnValueOnce(neverSettles())
      .mockResolvedValueOnce({ data: { user: { id: 'user-a' }, session: {} }, error: null });
    const url = 'altune://auth/recovery?token_hash=stalled-recovery&type=recovery';

    void completeAuthIntent(parseAuthLink(url), router, auth).catch(() => undefined);
    await jest.advanceTimersByTimeAsync(AUTH_ACTION_TIMEOUT_MS);
    const retry = completeAuthIntent(parseAuthLink(url), router, auth);

    expect(auth.verifyOtp).toHaveBeenCalledTimes(2);
    await expect(retry).resolves.toEqual({ kind: 'success' });
  });
});
