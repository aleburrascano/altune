import { useSignUp } from '../hooks/useSignUp';

import { createSupabaseAuthMock, runAsyncAuthHook } from './testUtils/authTestUtils';

jest.mock('@shared/auth/supabaseClient', () => ({ supabase: { auth: {} } }));

const { signUp: supabaseSignUp } = createSupabaseAuthMock('signUp');

const signUp = () => runAsyncAuthHook(useSignUp, (hook) => hook.signUp('a@b.co', 'pw'));

describe('useSignUp: mapping the resolved outcome of signUp', () => {
  it('maps a swallowed AuthRetryableFetchError to network', async () => {
    supabaseSignUp.mockResolvedValue({
      data: { user: null, session: null },
      error: { name: 'AuthRetryableFetchError', status: 0, message: 'Failed to fetch' },
    });

    expect(await signUp()).toEqual({ kind: 'error', reason: 'network' });
  });

  it('maps an AuthWeakPasswordError to weak_password', async () => {
    supabaseSignUp.mockResolvedValue({
      data: { user: null, session: null },
      error: { name: 'AuthWeakPasswordError', status: 422, code: 'weak_password', message: 'Password is too weak' },
    });

    expect(await signUp()).toEqual({ kind: 'error', reason: 'weak_password' });
  });

  it('maps an explicit user_already_exists error to already_registered', async () => {
    supabaseSignUp.mockResolvedValue({
      data: { user: null, session: null },
      error: { name: 'AuthApiError', status: 422, code: 'user_already_exists', message: 'User already registered' },
    });

    expect(await signUp()).toEqual({ kind: 'error', reason: 'already_registered' });
  });

  it('maps the empty-identities anti-enumeration signal to already_registered, not awaiting-confirmation', async () => {
    supabaseSignUp.mockResolvedValue({
      data: { user: { id: 'obfuscated', identities: [] }, session: null },
      error: null,
    });

    expect(await signUp()).toEqual({ kind: 'error', reason: 'already_registered' });
  });

  it('falls back to unknown when identities is null instead of an array', async () => {
    supabaseSignUp.mockResolvedValue({
      data: { user: { id: 'obfuscated', identities: null }, session: null },
      error: null,
    });

    expect(await signUp()).toEqual({ kind: 'error', reason: 'unknown' });
  });

  it('falls back to unknown when the user carries no identities at all', async () => {
    supabaseSignUp.mockResolvedValue({
      data: { user: { id: 'obfuscated' }, session: null },
      error: null,
    });

    expect(await signUp()).toEqual({ kind: 'error', reason: 'unknown' });
  });

  it('falls back to unknown when success carries neither a user nor a session', async () => {
    supabaseSignUp.mockResolvedValue({ data: { user: null, session: null }, error: null });

    expect(await signUp()).toEqual({ kind: 'error', reason: 'unknown' });
  });

  it('reports awaiting-confirmation for a fresh signup with identities but no session', async () => {
    supabaseSignUp.mockResolvedValue({
      data: { user: { id: 'new', identities: [{ id: 'i1' }] }, session: null },
      error: null,
    });

    expect(await signUp()).toEqual({ kind: 'awaiting-confirmation' });
  });

  it('reports ok when a session is returned immediately', async () => {
    supabaseSignUp.mockResolvedValue({
      data: { user: { id: 'new', identities: [{ id: 'i1' }] }, session: {} },
      error: null,
    });

    expect(await signUp()).toEqual({ kind: 'ok' });
  });

  it('reports ok for a session even when identities is an unexpected shape', async () => {
    supabaseSignUp.mockResolvedValue({
      data: { user: { id: 'new', identities: null }, session: {} },
      error: null,
    });

    expect(await signUp()).toEqual({ kind: 'ok' });
  });

  it('falls back to unknown for an unrecognised error code', async () => {
    supabaseSignUp.mockResolvedValue({
      data: { user: null, session: null },
      error: { name: 'AuthApiError', status: 400, code: 'validation_failed', message: 'Bad request' },
    });

    expect(await signUp()).toEqual({ kind: 'error', reason: 'unknown' });
  });
});

describe('a server rate limit', () => {
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

  describe.each([
    ['sign-up', supabaseSignUp, () => runAsyncAuthHook(useSignUp, (hook) => hook.signUp('a@b.co', 'pw'))],
  ] as const)('%s: a server rate limit is not a network error', (_name, mock, run) => {
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
});
