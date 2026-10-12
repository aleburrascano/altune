import { supabase } from '@shared/auth/supabaseClient';
import {
  authIntentClient,
  requestPasswordReset,
  signInWithPassword,
  signOutOtherSessions,
  signUp,
  startOAuth,
  updatePassword,
} from '@shared/auth/authActions';

type AuthMethod =
  | 'signInWithPassword'
  | 'signUp'
  | 'resetPasswordForEmail'
  | 'updateUser'
  | 'signOut'
  | 'signInWithOAuth';

const original = { ...supabase.auth };

function stub(method: AuthMethod, result: unknown): jest.Mock {
  const mock = jest.fn().mockResolvedValue(result);
  Object.assign(supabase.auth, { [method]: mock });
  return mock;
}

afterEach(() => {
  Object.assign(supabase.auth, original);
});

describe('authActions', () => {
  it('signInWithPassword forwards credentials and returns the result', async () => {
    const result = { data: {}, error: null };
    const mock = stub('signInWithPassword', result);
    const credentials = { email: 'a@b.co', password: 'pw' };
    await expect(signInWithPassword(credentials)).resolves.toBe(result);
    expect(mock).toHaveBeenCalledWith(credentials);
  });

  it('signUp forwards credentials and options and returns the result', async () => {
    const result = { data: {}, error: null };
    const mock = stub('signUp', result);
    const args = { email: 'a@b.co', password: 'pw', options: { emailRedirectTo: 'app://x' } };
    await expect(signUp(args)).resolves.toBe(result);
    expect(mock).toHaveBeenCalledWith(args);
  });

  it('requestPasswordReset forwards the email and options and returns the result', async () => {
    const result = { data: {}, error: null };
    const mock = stub('resetPasswordForEmail', result);
    await expect(requestPasswordReset('a@b.co', { redirectTo: 'app://r' })).resolves.toBe(result);
    expect(mock).toHaveBeenCalledWith('a@b.co', { redirectTo: 'app://r' });
  });

  it('updatePassword forwards the new password and returns the result', async () => {
    const result = { data: {}, error: null };
    const mock = stub('updateUser', result);
    await expect(updatePassword({ password: 'new' })).resolves.toBe(result);
    expect(mock).toHaveBeenCalledWith({ password: 'new' });
  });

  it('signOutOtherSessions signs out with the others scope and returns the result', async () => {
    const result = { error: null };
    const mock = stub('signOut', result);
    await expect(signOutOtherSessions()).resolves.toBe(result);
    expect(mock).toHaveBeenCalledWith({ scope: 'others' });
  });

  it('startOAuth forwards the provider options and returns the result', async () => {
    const result = { data: {}, error: null };
    const mock = stub('signInWithOAuth', result);
    const args = { provider: 'google' as const, options: { redirectTo: 'app://o' } };
    await expect(startOAuth(args)).resolves.toBe(result);
    expect(mock).toHaveBeenCalledWith(args);
  });

  it('authIntentClient returns the supabase auth client', () => {
    expect(authIntentClient()).toBe(supabase.auth);
  });
});
