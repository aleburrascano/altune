import { LOCKOUT_AFTER_FAILURES } from '../attemptLockout';
import { useSignIn } from '../hooks/useSignIn';

import { createSupabaseAuthMock, runAsyncAuthHook } from './testUtils/authTestUtils';

jest.mock('@shared/auth/supabaseClient', () => ({ supabase: { auth: {} } }));

const { signInWithPassword } = createSupabaseAuthMock('signInWithPassword');

const signInAs = (email: string) => runAsyncAuthHook(useSignIn, (hook) => hook.signIn(email, 'pw'));

const signIn = () => signInAs('a@b.co');

const WRONG_PASSWORD = {
  data: { user: null, session: null },
  error: {
    name: 'AuthApiError',
    status: 400,
    code: 'invalid_credentials',
    message: 'Invalid login credentials',
  },
};

const SIGNED_IN = { data: { user: {}, session: {} }, error: null };

describe('useSignIn: mapping the resolved { error } of signInWithPassword', () => {
  it('maps a swallowed AuthRetryableFetchError to network, never invalid_credentials', async () => {
    signInWithPassword.mockResolvedValue({
      data: { user: null, session: null },
      error: { name: 'AuthRetryableFetchError', status: 0, message: 'Failed to fetch' },
    });

    expect(await signIn()).toEqual({ kind: 'error', reason: 'network' });
  });

  it('maps a 503 reachability failure to network', async () => {
    signInWithPassword.mockResolvedValue({
      data: { user: null, session: null },
      error: { name: 'AuthApiError', status: 503, message: 'Service Unavailable' },
    });

    expect(await signIn()).toEqual({ kind: 'error', reason: 'network' });
  });

  it('maps a genuine 400 invalid_credentials to invalid_credentials', async () => {
    signInWithPassword.mockResolvedValue({
      data: { user: null, session: null },
      error: { name: 'AuthApiError', status: 400, code: 'invalid_credentials', message: 'Invalid login credentials' },
    });

    expect(await signIn()).toEqual({ kind: 'error', reason: 'invalid_credentials' });
  });

  // The password was right; only the address is unconfirmed. Saying "incorrect"
  // sends this user off to reset a password that was never the problem (#1646).
  it('maps an unconfirmed email to its own reason, not invalid_credentials', async () => {
    signInWithPassword.mockResolvedValue({
      data: { user: null, session: null },
      error: {
        name: 'AuthApiError',
        status: 400,
        code: 'email_not_confirmed',
        message: 'Email not confirmed',
      },
    });

    expect(await signIn()).toEqual({ kind: 'error', reason: 'email_not_confirmed' });
  });

  // Every GoTrue code we have not taught it — today's rate-limit-below-429,
  // tomorrow's new one — is a rejection of the request, not a verdict on the
  // password, so it must not be reported as one.
  it('maps an over_request_rate_limit rejection to too_many_attempts, not invalid_credentials', async () => {
    signInWithPassword.mockResolvedValue({
      data: { user: null, session: null },
      error: {
        name: 'AuthApiError',
        status: 400,
        code: 'over_request_rate_limit',
        message: 'Request rate limit reached',
      },
    });

    expect(await signIn()).toEqual({ kind: 'error', reason: 'too_many_attempts' });
  });

  it('reports ok when Supabase returns a session', async () => {
    signInWithPassword.mockResolvedValue({ data: { user: {}, session: {} }, error: null });

    expect(await signIn()).toEqual({ kind: 'ok' });
  });
});

describe('useSignIn: refusing a run of failures against one account (#1640)', () => {
  it('stops sending attempts to Supabase once the account is locked out', async () => {
    signInWithPassword.mockResolvedValue(WRONG_PASSWORD);
    for (let i = 0; i < LOCKOUT_AFTER_FAILURES; i += 1) await signIn();

    expect(await signIn()).toEqual({ kind: 'error', reason: 'too_many_attempts' });
    expect(signInWithPassword).toHaveBeenCalledTimes(LOCKOUT_AFTER_FAILURES);
  });

  // A lockout that outlived one account would hand an attacker a way to lock
  // every other user out of their own app, so it is keyed on the address.
  it('leaves a second account free while the first is locked out', async () => {
    signInWithPassword.mockResolvedValue(WRONG_PASSWORD);
    for (let i = 0; i < LOCKOUT_AFTER_FAILURES; i += 1) await signIn();

    expect(await signInAs('other@b.co')).toEqual({
      kind: 'error',
      reason: 'invalid_credentials',
    });
  });

  it('forgets the run once the right password lands, so the next typo is not a lockout', async () => {
    signInWithPassword.mockResolvedValue(WRONG_PASSWORD);
    for (let i = 0; i < LOCKOUT_AFTER_FAILURES - 1; i += 1) await signIn();
    signInWithPassword.mockResolvedValue(SIGNED_IN);
    await signIn();

    signInWithPassword.mockResolvedValue(WRONG_PASSWORD);
    expect(await signIn()).toEqual({ kind: 'error', reason: 'invalid_credentials' });
  });
});
