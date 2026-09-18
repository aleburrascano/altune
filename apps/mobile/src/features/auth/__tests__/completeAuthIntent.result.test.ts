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

    expect(result).toEqual({
      kind: 'failure',
      cause: 'gotrue_rejected',
      error: { name: 'AuthApiError', status: 401 },
    });
    expect(router.replace).not.toHaveBeenCalled();
  });

  it('does NOT navigate and reports failure when a recovery link carries no usable params', async () => {
    const url = 'altune://auth/recovery';

    const result = await completeAuthIntent(parseAuthLink(url), router, auth);

    expect(result).toEqual({ kind: 'failure', cause: 'no_spendable_credential' });
    expect(router.replace).not.toHaveBeenCalled();
  });

  it('reports failure when the OAuth code exchange resolves with { error }', async () => {
    auth.exchangeCodeForSession.mockResolvedValue({
      data: {},
      error: { name: 'AuthApiError', status: 400 },
    });
    const url = 'altune://auth/callback?code=bad-code';

    const result = await completeAuthIntent(parseAuthLink(url), router, auth);

    expect(result).toEqual({
      kind: 'failure',
      cause: 'gotrue_rejected',
      error: { name: 'AuthApiError', status: 400 },
    });
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

    const rejection = {
      kind: 'failure',
      cause: 'gotrue_rejected',
      error: { name: 'AuthApiError', status: 400 },
    };
    expect(await winner).toEqual(rejection);
    expect(await loser).toEqual(rejection);
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

    expect(result).toEqual({ kind: 'failure', cause: 'unhandled_intent_kind' });
    expect(auth.exchangeCodeForSession).not.toHaveBeenCalled();
  });

  it('reports ignored for a link that is not an auth link', async () => {
    const result = await completeAuthIntent(parseAuthLink('altune://library'), router, auth);

    expect(result).toEqual({ kind: 'ignored' });
  });
});

const EXPIRED_LINK_ERROR = {
  name: 'AuthApiError',
  code: 'otp_expired',
  status: 403,
  message: 'Email link is invalid or has expired',
};

const GOTRUE_UNREACHABLE_ERROR = {
  name: 'AuthRetryableFetchError',
  status: 0,
  message: 'Network request failed',
};

describe('completeAuthIntent: telling failures apart without a reproduction (#1647)', () => {
  it.each([
    [
      'a recovery link the server judged expired',
      { data: {}, error: EXPIRED_LINK_ERROR },
      'altune://auth/recovery?token_hash=expired-hash&type=recovery',
      { kind: 'failure', cause: 'gotrue_rejected', error: EXPIRED_LINK_ERROR },
    ],
    [
      'a recovery link sent while GoTrue was unreachable',
      { data: {}, error: GOTRUE_UNREACHABLE_ERROR },
      'altune://auth/recovery?token_hash=live-hash&type=recovery',
      { kind: 'failure', cause: 'gotrue_rejected', error: GOTRUE_UNREACHABLE_ERROR },
    ],
    [
      'a recovery link composed with a type this path may not spend',
      { data: {}, error: null },
      'altune://auth/recovery?token_hash=live-hash&type=magiclink',
      { kind: 'failure', cause: 'otp_type_not_allowed_for_path' },
    ],
    [
      'a recovery link carrying no token at all',
      { data: {}, error: null },
      'altune://auth/recovery?type=recovery',
      { kind: 'failure', cause: 'no_spendable_credential' },
    ],
  ])(
    'reports %s with a detail none of the other failures carry',
    async (_case, answer, url, expected) => {
      auth.verifyOtp.mockResolvedValue(answer);

      const result = await completeAuthIntent(parseAuthLink(url), router, auth);

      expect(result).toEqual(expected);
    },
  );

  it('keeps only the four named error fields, so nothing else on the SDK error rides along', async () => {
    // `requestBody` is not a field today's SDK sets. The point is that a field
    // added to the error later cannot reach a log just by being on the object,
    // and the credential is exactly what such a field would carry.
    auth.verifyOtp.mockResolvedValue({
      data: {},
      error: { name: 'AuthApiError', status: 401, requestBody: 'token_hash=super-secret-hash' },
    });
    const url = 'altune://auth/recovery?token_hash=super-secret-hash&type=recovery';

    const result = await completeAuthIntent(parseAuthLink(url), router, auth);

    expect(result).toEqual({
      kind: 'failure',
      cause: 'gotrue_rejected',
      error: { name: 'AuthApiError', status: 401 },
    });
    expect(JSON.stringify(result)).not.toContain('super-secret-hash');
  });
});
