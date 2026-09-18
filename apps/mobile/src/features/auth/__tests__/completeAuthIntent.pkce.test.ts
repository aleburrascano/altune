import { completeAuthIntent, _resetConsumedCredentialForTest } from '../completeAuthIntent';
import { parseAuthLink } from '../parseAuthLink';
import { clearRecoveryUnlock, isRecoveryUnlocked } from '../recoveryUnlock';

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

// What GoTrue's `/verify` redirect emits when an email template is left on the
// implicit flow: a live token pair delivered over the unverified `altune`
// scheme, replayable indefinitely by whatever intercepts it.
const IMPLICIT_TOKEN_PAIR = 'access_token=live-access&refresh_token=live-refresh&token_type=bearer';

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

      expect(result).toEqual({ kind: 'failure' });
      expect(auth.setSession).not.toHaveBeenCalled();
      expect(auth.verifyOtp).not.toHaveBeenCalled();
      expect(isRecoveryUnlocked()).toBe(false);
      expect(router.replace).not.toHaveBeenCalled();
    },
  );

  it('refuses a redelivered bare-token recovery link again, never laundering it as deduped', async () => {
    // `deduped` means "another delivery is establishing this session", which
    // useOAuth reads as success. A refusal must not become one by being seen
    // twice, so a refused link claims no credential.
    await completeAuthIntent(parseAuthLink(RECOVERY_IMPLICIT_LINK), router, auth);

    const result = await completeAuthIntent(parseAuthLink(RECOVERY_IMPLICIT_LINK), router, auth);

    expect(result).toEqual({ kind: 'failure' });
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
