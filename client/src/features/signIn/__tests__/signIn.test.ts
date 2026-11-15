jest.mock('decode-uri-component', () => ({ __esModule: true, default: (s: string) => s }));

describe('@features/signIn public surface', () => {
  it('exports exactly the ten public names', () => {
    expect(Object.keys(require('@features/signIn')).sort()).toEqual(
      [
        'AuthCallbackScreen',
        'AuthGate',
        'AuthPortProvider',
        'ForgotPasswordScreen',
        'SetNewPasswordScreen',
        'SignInScreen',
        'SignUpScreen',
        'TestAuthBridge',
        'httpAuthPort',
        'useAuthDeepLink',
      ].sort(),
    );
  });

  it('does not expose reportSignInFailure or completeAuthIntent', () => {
    const surface = require('@features/signIn');
    expect(surface.reportSignInFailure).toBeUndefined();
    expect(surface.completeAuthIntent).toBeUndefined();
  });
});
