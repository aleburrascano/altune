// Issue #1636: the deep-link path decides `intent.kind` and the query string
// decides the OTP `type` — both attacker-written. A link may only spend a
// credential whose type its own path is allowed to carry, so a signup or
// email-change token verified under an `auth/recovery` path can never unlock
// the reset-password screen.
import { completeAuthIntent, _resetConsumedCredentialForTest } from '../completeAuthIntent';
import { parseAuthLink } from '../parseAuthLink';
import { clearRecoveryUnlock, isRecoveryUnlocked } from '../recoveryUnlock';

// What the server answers a verification with: the account it attributed the
// token to. The unlock is bound to that id (#1638).
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

      expect(result).toEqual({ kind: 'failure' });
      expect(isRecoveryUnlocked(VERIFIED_USER)).toBe(false);
      expect(router.replace).not.toHaveBeenCalled();
    },
  );

  it('refuses a recovery-path link whose type differs from recovery only in case', async () => {
    const url = 'altune://auth/recovery?token_hash=genuine-hash&type=Recovery';

    const result = await completeAuthIntent(parseAuthLink(url), router, auth);

    expect(result).toEqual({ kind: 'failure' });
    expect(isRecoveryUnlocked(VERIFIED_USER)).toBe(false);
  });

  it('refuses a recovery-path link with a mismatched type rather than falling back to its token pair', async () => {
    const url =
      'altune://auth/recovery?token_hash=genuine-hash&type=email_change&access_token=a&refresh_token=r';

    const result = await completeAuthIntent(parseAuthLink(url), router, auth);

    expect(result).toEqual({ kind: 'failure' });
    expect(isRecoveryUnlocked(VERIFIED_USER)).toBe(false);
  });

  it('refuses a recovery token presented on the confirm path, leaving it unspent', async () => {
    const url = 'altune://auth/confirm?token_hash=genuine-hash&type=recovery';

    const result = await completeAuthIntent(parseAuthLink(url), router, auth);

    expect(result).toEqual({ kind: 'failure' });
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
