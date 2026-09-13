// Issue #656: the reset-password screen must unlock ONLY after a recovery
// verifyOtp/setSession actually succeeds — never for a failed link, a
// non-recovery intent, or a bare route hit that never reaches this code.
import { supabase } from '@shared/auth/supabaseClient';

import { completeAuthIntent } from '../lib/completeAuthIntent';
import { parseAuthLink } from '../lib/parseAuthLink';
import { clearRecoveryUnlock, isRecoveryUnlocked } from '../lib/recoveryUnlock';

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: {
    auth: {
      exchangeCodeForSession: jest.fn().mockResolvedValue({ data: {}, error: null }),
      setSession: jest.fn().mockResolvedValue({ data: {}, error: null }),
      verifyOtp: jest.fn().mockResolvedValue({ data: {}, error: null }),
    },
  },
}));

const verifyOtp = supabase.auth.verifyOtp as jest.Mock;
const exchangeCodeForSession = supabase.auth.exchangeCodeForSession as jest.Mock;
const router = { replace: jest.fn() };

beforeEach(() => {
  verifyOtp.mockReset().mockResolvedValue({ data: {}, error: null });
  exchangeCodeForSession.mockReset().mockResolvedValue({ data: {}, error: null });
  router.replace.mockReset();
  clearRecoveryUnlock();
});

afterEach(() => {
  clearRecoveryUnlock();
});

describe('completeAuthIntent unlocks the reset-password screen only on verified recovery', () => {
  it('unlocks after a clean recovery verifyOtp', async () => {
    const url = 'altune://auth/recovery?token_hash=ok&type=recovery';

    const result = await completeAuthIntent(parseAuthLink(url), router);

    expect(result).toEqual({ kind: 'success' });
    expect(isRecoveryUnlocked()).toBe(true);
  });

  it('does NOT unlock when the recovery verifyOtp fails', async () => {
    verifyOtp.mockResolvedValue({ data: {}, error: { name: 'AuthApiError', status: 401 } });
    const url = 'altune://auth/recovery?token_hash=bad&type=recovery';

    const result = await completeAuthIntent(parseAuthLink(url), router);

    expect(result).toEqual({ kind: 'failure' });
    expect(isRecoveryUnlocked()).toBe(false);
  });

  it('does NOT unlock for a successful OAuth callback', async () => {
    const url = 'altune://auth/callback?code=good';

    await completeAuthIntent(parseAuthLink(url), router);

    expect(isRecoveryUnlocked()).toBe(false);
  });

  it('does NOT unlock for a non-auth link', async () => {
    await completeAuthIntent(parseAuthLink('altune://library'), router);

    expect(isRecoveryUnlocked()).toBe(false);
  });
});
