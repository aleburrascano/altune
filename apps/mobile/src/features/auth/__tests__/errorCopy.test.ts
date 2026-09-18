import {
  NETWORK_ERROR_COPY,
  TOO_MANY_ATTEMPTS_COPY,
  authErrorText,
  type AuthErrorReason,
} from '../errorCopy';

const GENERIC = 'Email or password is incorrect.';

describe('authErrorText: the reasons that need their own words', () => {
  it('explains an unreachable server rather than blaming the credentials', () => {
    expect(authErrorText('network', GENERIC)).toBe(NETWORK_ERROR_COPY);
  });

  // The generic copy would tell a locked-out user their password is wrong, which
  // it is not — they would keep retyping a password that was right all along.
  it('explains the local lockout rather than blaming the credentials', () => {
    expect(authErrorText('too_many_attempts', GENERIC)).toBe(TOO_MANY_ATTEMPTS_COPY);
  });

  it('falls back to the caller-supplied copy for every other reason', () => {
    const others: AuthErrorReason[] = [
      'unknown',
      'invalid_credentials',
      'weak_password',
      'already_registered',
    ];

    expect(others.map((reason) => authErrorText(reason, GENERIC))).toEqual(
      others.map(() => GENERIC),
    );
  });
});
