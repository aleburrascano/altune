import { completeAuthIntent, _resetConsumedCredentialForTest } from '../completeAuthIntent';
import { parseAuthLink } from '../parseAuthLink';

const auth = {
  exchangeCodeForSession: jest.fn(),
  setSession: jest.fn(),
  verifyOtp: jest.fn(),
};

const router = { replace: jest.fn() };

beforeEach(() => {
  auth.exchangeCodeForSession.mockReset().mockResolvedValue({ data: {}, error: null });
  auth.setSession.mockReset().mockResolvedValue({ data: {}, error: null });
  auth.verifyOtp.mockReset().mockResolvedValue({ data: {}, error: null });
  router.replace.mockClear();
  _resetConsumedCredentialForTest();
});

describe('completeAuthIntent: the same OAuth callback delivered to two listeners', () => {
  it('exchanges the one-time code exactly once when both listeners fire concurrently', async () => {
    const url = 'altune://auth/callback?code=one-time-abc';

    // useOAuth (browser session result) and the global Linking listener both
    // hand the identical callback URL to completeAuthIntent, racing to consume
    // the single-use code.
    await Promise.all([
      completeAuthIntent(parseAuthLink(url), router, auth),
      completeAuthIntent(parseAuthLink(url), router, auth),
    ]);

    expect(auth.exchangeCodeForSession).toHaveBeenCalledTimes(1);
    expect(auth.exchangeCodeForSession).toHaveBeenCalledWith('one-time-abc');
  });

  it('exchanges the one-time code exactly once when the listeners fire sequentially', async () => {
    const url = 'altune://auth/callback?code=one-time-xyz';

    await completeAuthIntent(parseAuthLink(url), router, auth);
    await completeAuthIntent(parseAuthLink(url), router, auth);

    expect(auth.exchangeCodeForSession).toHaveBeenCalledTimes(1);
  });

  it('still exchanges a genuinely different code from a later sign-in', async () => {
    await completeAuthIntent(parseAuthLink('altune://auth/callback?code=first-code'), router, auth);
    await completeAuthIntent(
      parseAuthLink('altune://auth/callback?code=second-code'),
      router,
      auth,
    );

    expect(auth.exchangeCodeForSession).toHaveBeenCalledTimes(2);
    expect(auth.exchangeCodeForSession).toHaveBeenNthCalledWith(1, 'first-code');
    expect(auth.exchangeCodeForSession).toHaveBeenNthCalledWith(2, 'second-code');
  });

  it('reports deduped to the listener that lost the race once the winner succeeded', async () => {
    const url = 'altune://auth/callback?code=one-time-won';

    const [winner, loser] = await Promise.all([
      completeAuthIntent(parseAuthLink(url), router, auth),
      completeAuthIntent(parseAuthLink(url), router, auth),
    ]);

    expect(winner).toEqual({ kind: 'success' });
    expect(loser).toEqual({ kind: 'deduped' });
  });

  it('exchanges the same code again after the first exchange failed (#1641)', async () => {
    auth.exchangeCodeForSession
      .mockResolvedValueOnce({ data: {}, error: { name: 'AuthApiError', status: 503 } })
      .mockResolvedValueOnce({ data: {}, error: null });
    const url = 'altune://auth/callback?code=transiently-rejected';

    const first = await completeAuthIntent(parseAuthLink(url), router, auth);
    const second = await completeAuthIntent(parseAuthLink(url), router, auth);

    expect(first).toEqual({ kind: 'failure' });
    expect(second).toEqual({ kind: 'success' });
    expect(auth.exchangeCodeForSession).toHaveBeenCalledTimes(2);
  });

  it('verifies the same recovery token again after the first verification failed (#1641)', async () => {
    auth.verifyOtp
      .mockResolvedValueOnce({ data: {}, error: { name: 'AuthRetryableFetchError', status: 0 } })
      .mockResolvedValueOnce({ data: { user: { id: 'user-a' }, session: {} }, error: null });
    const url = 'altune://auth/recovery?token_hash=still-valid&type=recovery';

    const first = await completeAuthIntent(parseAuthLink(url), router, auth);
    const second = await completeAuthIntent(parseAuthLink(url), router, auth);

    expect(first).toEqual({ kind: 'failure' });
    expect(second).toEqual({ kind: 'success' });
    expect(auth.verifyOtp).toHaveBeenCalledTimes(2);
  });

  it('treats a code as unseen again once the claim is reset between tests', async () => {
    const url = 'altune://auth/callback?code=reused-across-tests';
    await completeAuthIntent(parseAuthLink(url), router, auth);

    _resetConsumedCredentialForTest();
    await completeAuthIntent(parseAuthLink(url), router, auth);

    expect(auth.exchangeCodeForSession).toHaveBeenCalledTimes(2);
  });
});
