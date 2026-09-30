import { completeAuthIntent, _resetConsumedCredentialForTest } from '../completeAuthIntent';
import { type AuthLinkIntent, parseAuthLink } from '../parseAuthLink';
import { clearRecoveryUnlock, isRecoveryUnlocked } from '../recoveryUnlock';
import { AUTH_ACTION_TIMEOUT_MS } from '../authDeadline';

describe('the outcome it reports', () => {
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
});

describe('PKCE-only credential exchange', () => {
  const VERIFIED_USER = 'user-a';

  const VERIFIED = { data: { user: { id: VERIFIED_USER }, session: {} }, error: null };

  const auth = {
    exchangeCodeForSession: jest.fn(),
    setSession: jest.fn(),
    verifyOtp: jest.fn(),
  };

  const router = { replace: jest.fn() };

  beforeEach(() => {
    auth.exchangeCodeForSession.mockReset().mockResolvedValue({ data: {}, error: null });
    auth.setSession.mockReset().mockResolvedValue({ data: {}, error: null });
    auth.verifyOtp.mockReset().mockResolvedValue(VERIFIED);
    router.replace.mockReset();
    clearRecoveryUnlock();
    _resetConsumedCredentialForTest();
  });

  afterEach(() => {
    clearRecoveryUnlock();
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
      const url =
        'altune://auth/callback#access_token=stolen-access&refresh_token=stolen-refresh&token_type=bearer';

      const result = await completeAuthIntent(parseAuthLink(url), router, auth);

      expect(result).toEqual({ kind: 'failure', cause: 'no_spendable_credential' });
      expect(auth.setSession).not.toHaveBeenCalled();
      expect(auth.exchangeCodeForSession).not.toHaveBeenCalled();
    });
  });

  const IMPLICIT_TOKEN_PAIR =
    'access_token=live-access&refresh_token=live-refresh&token_type=bearer';

  const RECOVERY_IMPLICIT_LINK = `altune://auth/recovery#${IMPLICIT_TOKEN_PAIR}&type=recovery`;

  const CONFIRM_IMPLICIT_LINK = `altune://auth/confirm#${IMPLICIT_TOKEN_PAIR}&type=signup`;

  describe('completeAuthIntent: recovery and confirm links are PKCE-only too (#1637)', () => {
    it.each([
      ['recovery', RECOVERY_IMPLICIT_LINK],
      ['confirm', CONFIRM_IMPLICIT_LINK],
    ])(
      'refuses an implicit-grant-shaped %s link rather than sessioning its bare tokens',
      async (_kind, url) => {
        const result = await completeAuthIntent(parseAuthLink(url), router, auth);

        expect(result).toEqual({ kind: 'failure', cause: 'no_spendable_credential' });
        expect(auth.setSession).not.toHaveBeenCalled();
        expect(auth.verifyOtp).not.toHaveBeenCalled();
        expect(isRecoveryUnlocked(VERIFIED_USER)).toBe(false);
        expect(router.replace).not.toHaveBeenCalled();
      },
    );

    it('refuses a redelivered bare-token recovery link again, never laundering it as deduped', async () => {
      await completeAuthIntent(parseAuthLink(RECOVERY_IMPLICIT_LINK), router, auth);

      const result = await completeAuthIntent(parseAuthLink(RECOVERY_IMPLICIT_LINK), router, auth);

      expect(result).toEqual({ kind: 'failure', cause: 'no_spendable_credential' });
      expect(auth.setSession).not.toHaveBeenCalled();
    });

    it('still verifies a recovery link whose token_hash arrives beside a stray token pair', async () => {
      const url = `altune://auth/recovery?token_hash=genuine-hash&type=recovery#${IMPLICIT_TOKEN_PAIR}`;

      const result = await completeAuthIntent(parseAuthLink(url), router, auth);

      expect(result).toEqual({ kind: 'success' });
      expect(auth.verifyOtp).toHaveBeenCalledWith({ type: 'recovery', token_hash: 'genuine-hash' });
      expect(auth.setSession).not.toHaveBeenCalled();
    });
  });
});

describe('binding the OTP type to the link path', () => {
  const VERIFIED_USER = 'user-a';

  const VERIFIED = { data: { user: { id: VERIFIED_USER }, session: {} }, error: null };

  const auth = {
    exchangeCodeForSession: jest.fn(),
    setSession: jest.fn(),
    verifyOtp: jest.fn(),
  };

  const router = { replace: jest.fn() };

  beforeEach(() => {
    auth.exchangeCodeForSession.mockReset().mockResolvedValue({ data: {}, error: null });
    auth.setSession.mockReset().mockResolvedValue({ data: {}, error: null });
    auth.verifyOtp.mockReset().mockResolvedValue(VERIFIED);
    router.replace.mockReset();
    clearRecoveryUnlock();
    _resetConsumedCredentialForTest();
  });

  afterEach(() => {
    clearRecoveryUnlock();
  });

  describe('completeAuthIntent binds the OTP type it verifies to the link path (#1636)', () => {
    it.each(['email_change', 'signup', 'email', 'magiclink', 'invite'])(
      'refuses a recovery-path link whose type says %s, even with a token the server would accept',
      async (type) => {
        const url = `altune://auth/recovery?token_hash=genuine-hash&type=${type}`;

        const result = await completeAuthIntent(parseAuthLink(url), router, auth);

        expect(result).toEqual({ kind: 'failure', cause: 'otp_type_not_allowed_for_path' });
        expect(isRecoveryUnlocked(VERIFIED_USER)).toBe(false);
        expect(router.replace).not.toHaveBeenCalled();
      },
    );

    it('refuses a recovery-path link whose type differs from recovery only in case', async () => {
      const url = 'altune://auth/recovery?token_hash=genuine-hash&type=Recovery';

      const result = await completeAuthIntent(parseAuthLink(url), router, auth);

      expect(result).toEqual({ kind: 'failure', cause: 'otp_type_not_allowed_for_path' });
      expect(isRecoveryUnlocked(VERIFIED_USER)).toBe(false);
    });

    it('refuses a recovery-path link with a mismatched type rather than falling back to its token pair', async () => {
      const url =
        'altune://auth/recovery?token_hash=genuine-hash&type=email_change&access_token=a&refresh_token=r';

      const result = await completeAuthIntent(parseAuthLink(url), router, auth);

      expect(result).toEqual({ kind: 'failure', cause: 'otp_type_not_allowed_for_path' });
      expect(isRecoveryUnlocked(VERIFIED_USER)).toBe(false);
    });

    it('refuses a recovery token presented on the confirm path, leaving it unspent', async () => {
      const url = 'altune://auth/confirm?token_hash=genuine-hash&type=recovery';

      const result = await completeAuthIntent(parseAuthLink(url), router, auth);

      expect(result).toEqual({ kind: 'failure', cause: 'otp_type_not_allowed_for_path' });
      expect(auth.verifyOtp).not.toHaveBeenCalled();
    });

    it('spends a recovery link as a recovery token and nothing else', async () => {
      const url = 'altune://auth/recovery?token_hash=genuine-hash&type=recovery';

      const result = await completeAuthIntent(parseAuthLink(url), router, auth);

      expect(result).toEqual({ kind: 'success' });
      expect(auth.verifyOtp).toHaveBeenCalledWith({
        type: 'recovery',
        token_hash: 'genuine-hash',
      });
      expect(isRecoveryUnlocked(VERIFIED_USER)).toBe(true);
    });

    it.each(['signup', 'email'])(
      'verifies a confirm-path link carrying a %s token without unlocking recovery',
      async (type) => {
        const url = `altune://auth/confirm?token_hash=genuine-hash&type=${type}`;

        const result = await completeAuthIntent(parseAuthLink(url), router, auth);

        expect(result).toEqual({ kind: 'success' });
        expect(auth.verifyOtp).toHaveBeenCalledWith({ type, token_hash: 'genuine-hash' });
        expect(isRecoveryUnlocked(VERIFIED_USER)).toBe(false);
      },
    );
  });
});

describe('unlocking the reset-password screen', () => {
  const VERIFIED_USER = 'user-a';

  const OTHER_USER = 'user-b';

  function verified(userId: string | null) {
    return { data: { user: userId === null ? null : { id: userId }, session: {} }, error: null };
  }

  const auth = {
    exchangeCodeForSession: jest.fn(),
    setSession: jest.fn(),
    verifyOtp: jest.fn(),
  };

  const router = { replace: jest.fn() };

  beforeEach(() => {
    auth.exchangeCodeForSession.mockReset().mockResolvedValue({ data: {}, error: null });
    auth.setSession.mockReset().mockResolvedValue({ data: {}, error: null });
    auth.verifyOtp.mockReset().mockResolvedValue(verified(VERIFIED_USER));
    router.replace.mockReset();
    clearRecoveryUnlock();
    _resetConsumedCredentialForTest();
  });

  afterEach(() => {
    clearRecoveryUnlock();
  });

  describe('completeAuthIntent unlocks the reset-password screen only on verified recovery', () => {
    it('unlocks after a clean recovery verifyOtp', async () => {
      const url = 'altune://auth/recovery?token_hash=ok&type=recovery';

      const result = await completeAuthIntent(parseAuthLink(url), router, auth);

      expect(result).toEqual({ kind: 'success' });
      expect(isRecoveryUnlocked(VERIFIED_USER)).toBe(true);
    });

    it('does NOT unlock when the recovery verifyOtp fails', async () => {
      auth.verifyOtp.mockResolvedValue({ data: {}, error: { name: 'AuthApiError', status: 401 } });
      const url = 'altune://auth/recovery?token_hash=bad&type=recovery';

      const result = await completeAuthIntent(parseAuthLink(url), router, auth);

      expect(result).toEqual({
        kind: 'failure',
        cause: 'gotrue_rejected',
        error: { name: 'AuthApiError', status: 401 },
      });
      expect(isRecoveryUnlocked(VERIFIED_USER)).toBe(false);
    });

    it('does NOT unlock for a successful OAuth callback', async () => {
      const url = 'altune://auth/callback?code=good';

      await completeAuthIntent(parseAuthLink(url), router, auth);

      expect(isRecoveryUnlocked(VERIFIED_USER)).toBe(false);
    });

    it('does NOT unlock for a non-auth link', async () => {
      await completeAuthIntent(parseAuthLink('altune://library'), router, auth);

      expect(isRecoveryUnlocked(VERIFIED_USER)).toBe(false);
    });
  });

  describe('completeAuthIntent binds the unlock to the user the server verified (#1638)', () => {
    it('unlocks for the verified user and for no other account', async () => {
      const url = 'altune://auth/recovery?token_hash=ok&type=recovery';

      await completeAuthIntent(parseAuthLink(url), router, auth);

      expect(isRecoveryUnlocked(VERIFIED_USER)).toBe(true);
      expect(isRecoveryUnlocked(OTHER_USER)).toBe(false);
    });

    it('takes the identity from the verification response, not from the link', async () => {
      auth.verifyOtp.mockResolvedValue(verified(OTHER_USER));
      const url = 'altune://auth/recovery?token_hash=ok&type=recovery';

      await completeAuthIntent(parseAuthLink(url), router, auth);

      expect(isRecoveryUnlocked(OTHER_USER)).toBe(true);
      expect(isRecoveryUnlocked(VERIFIED_USER)).toBe(false);
    });

    it('fails closed, unlocking for nobody, when a verified recovery names no user', async () => {
      auth.verifyOtp.mockResolvedValue(verified(null));
      const url = 'altune://auth/recovery?token_hash=ok&type=recovery';

      const result = await completeAuthIntent(parseAuthLink(url), router, auth);

      expect(result).toEqual({ kind: 'failure', cause: 'verification_named_no_user' });
      expect(isRecoveryUnlocked(VERIFIED_USER)).toBe(false);
      expect(router.replace).not.toHaveBeenCalled();
    });
  });
});

describe('spending a one-time credential once', () => {
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
      await completeAuthIntent(
        parseAuthLink('altune://auth/callback?code=first-code'),
        router,
        auth,
      );
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

      expect(first).toEqual({
        kind: 'failure',
        cause: 'gotrue_rejected',
        error: { name: 'AuthApiError', status: 503 },
      });
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

      expect(first).toEqual({
        kind: 'failure',
        cause: 'gotrue_rejected',
        error: { name: 'AuthRetryableFetchError', status: 0 },
      });
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
});

describe('a verification that outlives the auth deadline', () => {
  const auth = {
    exchangeCodeForSession: jest.fn(),
    verifyOtp: jest.fn(),
  };
  const router = { replace: jest.fn() };
  const url = 'altune://auth/recovery?token_hash=slow-recovery&type=recovery';

  function slowVerification() {
    let settle!: (value: unknown) => void;
    auth.verifyOtp.mockReturnValueOnce(new Promise((resolve) => (settle = resolve)));
    return (value: unknown) => settle(value);
  }

  async function pastDeadline() {
    const timedOut = completeAuthIntent(parseAuthLink(url), router, auth).catch(() => undefined);
    await jest.advanceTimersByTimeAsync(AUTH_ACTION_TIMEOUT_MS);
    await timedOut;
  }

  beforeEach(() => {
    jest.useFakeTimers();
    auth.verifyOtp.mockReset();
    router.replace.mockReset();
    _resetConsumedCredentialForTest();
  });

  afterEach(() => {
    jest.useRealTimers();
  });

  describe('completeAuthIntent: guarding a spend the deadline abandoned (#448)', () => {
    it('dedupes a re-delivered link within its own deadline without a second verifyOtp', async () => {
      slowVerification();
      await pastDeadline();

      const redelivered = completeAuthIntent(parseAuthLink(url), router, auth);
      const outcome = expect(redelivered).rejects.toMatchObject({ failure: 'timeout' });
      await jest.advanceTimersByTimeAsync(AUTH_ACTION_TIMEOUT_MS);
      await outcome;

      expect(auth.verifyOtp).toHaveBeenCalledTimes(1);
    });

    it('returns deduped to a re-delivery once the slow verification succeeds', async () => {
      const settle = slowVerification();
      await pastDeadline();

      settle({ data: { user: { id: 'user-a' }, session: {} }, error: null });
      await jest.advanceTimersByTimeAsync(0);
      const redelivered = await completeAuthIntent(parseAuthLink(url), router, auth);

      expect(redelivered).toEqual({ kind: 'deduped' });
      expect(auth.verifyOtp).toHaveBeenCalledTimes(1);
    });

    it('releases the credential for a retry when the slow verification later fails', async () => {
      const settle = slowVerification();
      await pastDeadline();
      void completeAuthIntent(parseAuthLink(url), router, auth).catch(() => undefined);
      expect(auth.verifyOtp).toHaveBeenCalledTimes(1);

      settle({ data: {}, error: { name: 'AuthApiError', status: 500 } });
      await jest.advanceTimersByTimeAsync(0);
      auth.verifyOtp.mockResolvedValueOnce({
        data: { user: { id: 'user-a' }, session: {} },
        error: null,
      });
      const retry = await completeAuthIntent(parseAuthLink(url), router, auth);

      expect(retry).toEqual({ kind: 'success' });
      expect(auth.verifyOtp).toHaveBeenCalledTimes(2);
    });
  });
});

describe('an exchange that never settles', () => {
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

    it('holds the claim on a stalled code exchange past the deadline and does not re-spend the retapped link', async () => {
      auth.exchangeCodeForSession.mockReturnValueOnce(neverSettles());
      const url = 'altune://auth/callback?code=stalled-then-retapped';

      const stalledRejection = expect(
        completeAuthIntent(parseAuthLink(url), router, auth),
      ).rejects.toMatchObject({ name: 'NetworkError', failure: 'timeout' });
      await jest.advanceTimersByTimeAsync(AUTH_ACTION_TIMEOUT_MS);
      await stalledRejection;
      const retry = expect(
        completeAuthIntent(parseAuthLink(url), router, auth),
      ).rejects.toMatchObject({
        name: 'NetworkError',
        failure: 'timeout',
      });
      await jest.advanceTimersByTimeAsync(AUTH_ACTION_TIMEOUT_MS);
      await retry;

      expect(auth.exchangeCodeForSession).toHaveBeenCalledTimes(1);
    });

    it('holds the claim on a stalled recovery verification past the deadline and does not re-verify the retapped link', async () => {
      auth.verifyOtp.mockReturnValueOnce(neverSettles());
      const url = 'altune://auth/recovery?token_hash=stalled-recovery&type=recovery';

      void completeAuthIntent(parseAuthLink(url), router, auth).catch(() => undefined);
      await jest.advanceTimersByTimeAsync(AUTH_ACTION_TIMEOUT_MS);
      const retry = expect(
        completeAuthIntent(parseAuthLink(url), router, auth),
      ).rejects.toMatchObject({
        name: 'NetworkError',
        failure: 'timeout',
      });
      await jest.advanceTimersByTimeAsync(AUTH_ACTION_TIMEOUT_MS);
      await retry;

      expect(auth.verifyOtp).toHaveBeenCalledTimes(1);
      expect(router.replace).not.toHaveBeenCalled();
    });
  });
});
