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
  router.replace.mockReset();
  _resetConsumedCredentialForTest();
});

describe('completeAuthIntent: OAuth callbacks exchange a PKCE code, never trust inline tokens (#655)', () => {
  it('exchanges the single-use code for a session on a PKCE callback', async () => {
    const url = 'altune://auth/callback?code=pkce-code-123';

    const result = await completeAuthIntent(parseAuthLink(url), router, auth);

    expect(result).toEqual({ kind: 'success' });
    expect(auth.exchangeCodeForSession).toHaveBeenCalledWith('pkce-code-123');
    expect(auth.setSession).not.toHaveBeenCalled();
  });

  it('rejects a captured implicit-grant callback without calling setSession on its bare tokens', async () => {
    // The old implicit-flow shape an interceptor could replay: live token pair
    // embedded in the redirect fragment. Under PKCE the callback carries only a
    // `code`, so this must be refused rather than trusted.
    const url =
      'altune://auth/callback#access_token=stolen-access&refresh_token=stolen-refresh&token_type=bearer';

    const result = await completeAuthIntent(parseAuthLink(url), router, auth);

    expect(result).toEqual({ kind: 'failure' });
    expect(auth.setSession).not.toHaveBeenCalled();
    expect(auth.exchangeCodeForSession).not.toHaveBeenCalled();
  });
});
