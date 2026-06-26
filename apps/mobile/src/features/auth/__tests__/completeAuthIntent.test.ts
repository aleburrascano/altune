/**
 * completeAuthIntent — exchanges parsed deep-link tokens for a Supabase
 * session and routes recovery to set-new-password. SDK + router mocked.
 */
/* eslint-disable @typescript-eslint/no-require-imports */
const mockVerifyOtp = jest.fn();
const mockSetSession = jest.fn();
const mockExchange = jest.fn();

jest.mock('../api/supabaseClient', () => ({
  supabase: {
    auth: {
      verifyOtp: (a: unknown) => mockVerifyOtp(a),
      setSession: (a: unknown) => mockSetSession(a),
      exchangeCodeForSession: (a: unknown) => mockExchange(a),
    },
  },
}));

const { completeAuthIntent } = require('../hooks/useAuthDeepLink');

beforeEach(() => {
  mockVerifyOtp.mockReset().mockResolvedValue({ error: null });
  mockSetSession.mockReset().mockResolvedValue({ error: null });
  mockExchange.mockReset().mockResolvedValue({ error: null });
});

describe('completeAuthIntent', () => {
  it('does nothing for an ignored intent', async () => {
    const router = { replace: jest.fn() };
    await completeAuthIntent({ kind: 'ignored' }, router);
    expect(mockVerifyOtp).not.toHaveBeenCalled();
    expect(router.replace).not.toHaveBeenCalled();
  });

  it('verifies a recovery token_hash and routes to set-new-password', async () => {
    const router = { replace: jest.fn() };
    await completeAuthIntent(
      { kind: 'recovery', params: { token_hash: 'tok', type: 'recovery' } },
      router,
    );
    expect(mockVerifyOtp).toHaveBeenCalledWith({ type: 'recovery', token_hash: 'tok' });
    expect(router.replace).toHaveBeenCalledWith('/reset-password');
  });

  it('verifies a confirm token without navigating (AuthGate routes the session)', async () => {
    const router = { replace: jest.fn() };
    await completeAuthIntent(
      { kind: 'confirm', params: { token_hash: 'tok', type: 'signup' } },
      router,
    );
    expect(mockVerifyOtp).toHaveBeenCalledWith({ type: 'signup', token_hash: 'tok' });
    expect(router.replace).not.toHaveBeenCalled();
  });

  it('sets a session from fragment tokens when no token_hash is present', async () => {
    const router = { replace: jest.fn() };
    await completeAuthIntent(
      { kind: 'confirm', params: { access_token: 'a', refresh_token: 'r' } },
      router,
    );
    expect(mockSetSession).toHaveBeenCalledWith({ access_token: 'a', refresh_token: 'r' });
  });

  it('exchanges an OAuth PKCE code for a session', async () => {
    const router = { replace: jest.fn() };
    await completeAuthIntent({ kind: 'oauth', params: { code: 'authcode' } }, router);
    expect(mockExchange).toHaveBeenCalledWith('authcode');
  });
});
