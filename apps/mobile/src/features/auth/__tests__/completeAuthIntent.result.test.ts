import { completeAuthIntent, _resetConsumedCredentialForTest } from '../completeAuthIntent';
import { type AuthLinkIntent, parseAuthLink } from '../parseAuthLink';

const auth = {
  exchangeCodeForSession: jest.fn(),
  setSession: jest.fn(),
  verifyOtp: jest.fn(),
};

const router = { replace: jest.fn() };

beforeEach(() => {
  auth.exchangeCodeForSession.mockReset().mockResolvedValue({ data: {}, error: null });
  auth.setSession.mockReset().mockResolvedValue({ data: {}, error: null });
  auth.verifyOtp.mockReset().mockResolvedValue({
    data: { user: { id: 'user-a' }, session: {} },
    error: null,
  });
  router.replace.mockReset();
  _resetConsumedCredentialForTest();
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

  it('reports the winner’s failure to the delivery that lost the race, never deduped (#1641)', async () => {
    // The two listeners are both in flight before the exchange settles, so the
    // loser can only learn the outcome by awaiting what the winner started.
    let settleExchange = (_result: { data: object; error: object | null }): void => undefined;
    auth.exchangeCodeForSession.mockReturnValue(
      new Promise((resolve) => {
        settleExchange = resolve;
      }),
    );
    const url = 'altune://auth/callback?code=one-time-doomed';

    const winner = completeAuthIntent(parseAuthLink(url), router, auth);
    const loser = completeAuthIntent(parseAuthLink(url), router, auth);
    settleExchange({ data: {}, error: { name: 'AuthApiError', status: 400 } });

    expect(await winner).toEqual({ kind: 'failure' });
    expect(await loser).toEqual({ kind: 'failure' });
  });

  it('refuses an intent kind it has no branch for instead of exchanging it as OAuth (#1644)', async () => {
    // The compiler now rejects an unhandled kind outright; this is the runtime
    // half of the same guard — a kind it has no plan for must not inherit the
    // PKCE exchange, which is what the old implicit else handed it.
    const unplannedKind = { kind: 'magiclink', params: { code: 'not-an-oauth-code' } };

    const result = await completeAuthIntent(
      unplannedKind as unknown as AuthLinkIntent,
      router,
      auth,
    );

    expect(result).toEqual({ kind: 'failure' });
    expect(auth.exchangeCodeForSession).not.toHaveBeenCalled();
  });

  it('reports ignored for a link that is not an auth link', async () => {
    const result = await completeAuthIntent(parseAuthLink('altune://library'), router, auth);

    expect(result).toEqual({ kind: 'ignored' });
  });
});
