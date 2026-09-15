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
const setSession = supabase.auth.setSession as jest.Mock;

const router = { replace: jest.fn() };

beforeEach(() => {
  exchangeCodeForSession.mockReset().mockResolvedValue({ data: {}, error: null });
  setSession.mockReset().mockResolvedValue({ data: {}, error: null });
  router.replace.mockReset();
});

describe('completeAuthIntent: OAuth callbacks exchange a PKCE code, never trust inline tokens (#655)', () => {
  it('exchanges the single-use code for a session on a PKCE callback', async () => {
    const url = 'altune://auth/callback?code=pkce-code-123';

    const result = await completeAuthIntent(parseAuthLink(url), router);

    expect(result).toEqual({ kind: 'success' });
    expect(exchangeCodeForSession).toHaveBeenCalledWith('pkce-code-123');
    expect(setSession).not.toHaveBeenCalled();
  });

  it('rejects a captured implicit-grant callback without calling setSession on its bare tokens', async () => {
    // The old implicit-flow shape an interceptor could replay: live token pair
    // embedded in the redirect fragment. Under PKCE the callback carries only a
    // `code`, so this must be refused rather than trusted.
    const url =
      'altune://auth/callback#access_token=stolen-access&refresh_token=stolen-refresh&token_type=bearer';

    const result = await completeAuthIntent(parseAuthLink(url), router);

    expect(result).toEqual({ kind: 'failure' });
    expect(setSession).not.toHaveBeenCalled();
    expect(exchangeCodeForSession).not.toHaveBeenCalled();
  });
});
