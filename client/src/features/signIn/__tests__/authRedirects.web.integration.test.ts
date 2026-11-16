import {
  authRedirectUrl,
  createSupabaseAuthMock,
  parseAuthLink,
  runAsyncAuthHook,
  useResetPassword,
  useSignUp,
} from '@features/signIn';

const { signUp, resetPasswordForEmail } = createSupabaseAuthMock('signUp', 'resetPasswordForEmail');

function pageOrigin(): string {
  return window.location.origin;
}

describe('auth redirects on web come back to the page origin (#2837)', () => {
  it('builds every redirect from the page origin instead of the altune scheme', () => {
    expect(authRedirectUrl('callback')).toBe(`${pageOrigin()}/auth/callback`);
    expect(authRedirectUrl('confirm')).toBe(`${pageOrigin()}/auth/confirm`);
    expect(authRedirectUrl('recovery')).toBe(`${pageOrigin()}/auth/recovery`);
  });

  it('accepts a link at the page origin and ignores the same path elsewhere', () => {
    expect(parseAuthLink(`${pageOrigin()}/auth/callback?code=xyz`)).toEqual({
      kind: 'oauth',
      params: { code: 'xyz' },
    });
    expect(parseAuthLink('https://evil.example/auth/callback?code=xyz')).toEqual({
      kind: 'ignored',
    });
  });

  it('emails the sign-up confirmation link back to the page origin', async () => {
    signUp.mockResolvedValue({ data: { user: null, session: {} }, error: null });

    await runAsyncAuthHook(useSignUp, (hook) => hook.signUp('a@b.co', 'pw'));

    expect(signUp).toHaveBeenCalledWith(
      expect.objectContaining({
        options: { emailRedirectTo: `${pageOrigin()}/auth/confirm` },
      }),
    );
  });

  it('sends the password recovery link back to the page origin', async () => {
    resetPasswordForEmail.mockResolvedValue({ data: {}, error: null });

    await runAsyncAuthHook(useResetPassword, (hook) => hook.requestReset('a@b.co'));

    expect(resetPasswordForEmail).toHaveBeenCalledWith('a@b.co', {
      redirectTo: `${pageOrigin()}/auth/recovery`,
    });
  });
});
