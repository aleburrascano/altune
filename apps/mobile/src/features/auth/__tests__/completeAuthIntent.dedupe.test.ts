import { completeAuthIntent } from '../completeAuthIntent';
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
});
