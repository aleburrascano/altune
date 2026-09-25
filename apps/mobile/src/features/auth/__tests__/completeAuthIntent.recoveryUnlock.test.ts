// Issue #656: the reset-password screen must unlock ONLY after a recovery
// verifyOtp actually succeeds — never for a failed link, a
// non-recovery intent, or a bare route hit that never reaches this code.
// Issue #1638: and only for the account the server named on that verification.
import { completeAuthIntent, _resetConsumedCredentialForTest } from '../completeAuthIntent';
import { parseAuthLink } from '../parseAuthLink';
import { clearRecoveryUnlock, isRecoveryUnlocked } from '../recoveryUnlock';

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
