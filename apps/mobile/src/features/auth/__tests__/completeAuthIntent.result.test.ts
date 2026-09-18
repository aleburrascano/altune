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
  router.replace.mockReset();
});

describe('completeAuthIntent: reporting whether the exchange actually succeeded (#657)', () => {
  it('navigates to /reset-password and reports success when verifyOtp resolves cleanly', async () => {
    const url = 'altune://auth/recovery?token_hash=ok-1&type=recovery';

    const result = await completeAuthIntent(parseAuthLink(url), router, auth);

    expect(result).toEqual({ kind: 'success' });
    expect(router.replace).toHaveBeenCalledWith('/reset-password');
  });

  it('does NOT navigate and reports failure when verifyOtp resolves with { error }', async () => {
    auth.verifyOtp.mockResolvedValue({ data: {}, error: { name: 'AuthApiError', status: 401 } });
    const url = 'altune://auth/recovery?token_hash=bad-1&type=recovery';

    const result = await completeAuthIntent(parseAuthLink(url), router, auth);

    expect(result).toEqual({ kind: 'failure' });
    expect(router.replace).not.toHaveBeenCalled();
  });

  it('does NOT navigate and reports failure when a recovery link carries no usable params', async () => {
    const url = 'altune://auth/recovery';

    const result = await completeAuthIntent(parseAuthLink(url), router, auth);

    expect(result).toEqual({ kind: 'failure' });
    expect(router.replace).not.toHaveBeenCalled();
  });

  it('reports failure when the OAuth code exchange resolves with { error }', async () => {
    auth.exchangeCodeForSession.mockResolvedValue({
      data: {},
      error: { name: 'AuthApiError', status: 400 },
    });
    const url = 'altune://auth/callback?code=bad-code';

    const result = await completeAuthIntent(parseAuthLink(url), router, auth);

    expect(result).toEqual({ kind: 'failure' });
  });

  it('reports success for a clean OAuth code exchange without navigating', async () => {
    const url = 'altune://auth/callback?code=good-code';

    const result = await completeAuthIntent(parseAuthLink(url), router, auth);

    expect(result).toEqual({ kind: 'success' });
    expect(router.replace).not.toHaveBeenCalled();
  });

  it('reports ignored for a link that is not an auth link', async () => {
    const result = await completeAuthIntent(parseAuthLink('altune://library'), router, auth);

    expect(result).toEqual({ kind: 'ignored' });
  });
});
