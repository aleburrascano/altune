import { supabase } from '@shared/auth/supabaseClient';

import { completeAuthIntent } from '../completeAuthIntent';
import { parseAuthLink } from '../parseAuthLink';

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: {
    auth: {
      exchangeCodeForSession: jest.fn().mockResolvedValue({ data: {}, error: null }),
      setSession: jest.fn().mockResolvedValue({ data: {}, error: null }),
      verifyOtp: jest.fn().mockResolvedValue({ data: {}, error: null }),
    },
  },
}));

const exchangeCodeForSession = supabase.auth.exchangeCodeForSession as jest.Mock;

const router = { replace: jest.fn() };

beforeEach(() => {
  exchangeCodeForSession.mockClear();
  router.replace.mockClear();
});

describe('completeAuthIntent: the same OAuth callback delivered to two listeners', () => {
  it('exchanges the one-time code exactly once when both listeners fire concurrently', async () => {
    const url = 'altune://auth/callback?code=one-time-abc';

    // useOAuth (browser session result) and the global Linking listener both
    // hand the identical callback URL to completeAuthIntent, racing to consume
    // the single-use code.
    await Promise.all([
      completeAuthIntent(parseAuthLink(url), router),
      completeAuthIntent(parseAuthLink(url), router),
    ]);

    expect(exchangeCodeForSession).toHaveBeenCalledTimes(1);
    expect(exchangeCodeForSession).toHaveBeenCalledWith('one-time-abc');
  });

  it('exchanges the one-time code exactly once when the listeners fire sequentially', async () => {
    const url = 'altune://auth/callback?code=one-time-xyz';

    await completeAuthIntent(parseAuthLink(url), router);
    await completeAuthIntent(parseAuthLink(url), router);

    expect(exchangeCodeForSession).toHaveBeenCalledTimes(1);
  });

  it('still exchanges a genuinely different code from a later sign-in', async () => {
    await completeAuthIntent(parseAuthLink('altune://auth/callback?code=first-code'), router);
    await completeAuthIntent(parseAuthLink('altune://auth/callback?code=second-code'), router);

    expect(exchangeCodeForSession).toHaveBeenCalledTimes(2);
    expect(exchangeCodeForSession).toHaveBeenNthCalledWith(1, 'first-code');
    expect(exchangeCodeForSession).toHaveBeenNthCalledWith(2, 'second-code');
  });
});
