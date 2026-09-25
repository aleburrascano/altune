import { useResetPassword } from '../hooks/useResetPassword';
import { useSignIn } from '../hooks/useSignIn';
import { useSignUp } from '../hooks/useSignUp';
import { useUpdatePassword } from '../hooks/useUpdatePassword';

import { createSupabaseAuthMock, runAsyncAuthHook } from './testUtils/authTestUtils';

jest.mock('@shared/auth/supabaseClient', () => ({ supabase: { auth: {} } }));

const { resetPasswordForEmail, signInWithPassword, signUp, updateUser } = createSupabaseAuthMock(
  'resetPasswordForEmail',
  'signInWithPassword',
  'signUp',
  'updateUser',
);

const RATE_LIMITED = {
  data: null,
  error: { name: 'AuthApiError', status: 429, code: 'over_email_send_rate_limit', message: 'limit' },
};
const RATE_LIMITED_BY_CODE = {
  data: null,
  error: { name: 'AuthApiError', status: 400, code: 'over_request_rate_limit', message: 'limit' },
};
const UNAVAILABLE = {
  data: null,
  error: { name: 'AuthApiError', status: 503, message: 'down' },
};
const RETRYABLE_429 = {
  data: null,
  error: { name: 'AuthRetryableFetchError', status: 429, message: 'limit' },
};

const actions = [
  [
    'reset',
    resetPasswordForEmail,
    () => runAsyncAuthHook(useResetPassword, (hook) => hook.requestReset('a@b.co')),
  ],
  [
    'sign-in',
    signInWithPassword,
    () => runAsyncAuthHook(useSignIn, (hook) => hook.signIn('a@b.co', 'pw')),
  ],
  ['sign-up', signUp, () => runAsyncAuthHook(useSignUp, (hook) => hook.signUp('a@b.co', 'pw'))],
  [
    'update-password',
    updateUser,
    () => runAsyncAuthHook(useUpdatePassword, (hook) => hook.updatePassword('pw')),
  ],
] as const;

describe.each(actions)('%s: a server rate limit is not a network error', (_name, mock, run) => {
  it('maps a resolved 429 to too_many_attempts', async () => {
    mock.mockResolvedValue(RATE_LIMITED);

    expect(await run()).toEqual({ kind: 'error', reason: 'too_many_attempts' });
  });

  it('maps an over_*_rate_limit code on another status to too_many_attempts', async () => {
    mock.mockResolvedValue(RATE_LIMITED_BY_CODE);

    expect(await run()).toEqual({ kind: 'error', reason: 'too_many_attempts' });
  });

  it('maps a 429 the SDK wrapped as a retryable fetch error to too_many_attempts', async () => {
    mock.mockResolvedValue(RETRYABLE_429);

    expect(await run()).toEqual({ kind: 'error', reason: 'too_many_attempts' });
  });

  it('still maps a 503 to network', async () => {
    mock.mockResolvedValue(UNAVAILABLE);

    expect(await run()).toEqual({ kind: 'error', reason: 'network' });
  });
});
