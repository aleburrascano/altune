import {
  EMAIL_NOT_CONFIRMED_COPY,
  INVALID_SIGN_IN_COPY,
  NETWORK_ERROR_COPY,
  TOO_MANY_ATTEMPTS_COPY,
  authErrorText,
  type AuthErrorReason,
} from '../errorReason';

const GENERIC = "Couldn't sign you in. Please try again.";

describe('authErrorText: the reasons that need their own words', () => {
  it('explains an unreachable server rather than blaming the credentials', () => {
    expect(authErrorText('network', GENERIC)).toBe(NETWORK_ERROR_COPY);
  });

  it('explains the local lockout rather than blaming the credentials', () => {
    expect(authErrorText('too_many_attempts', GENERIC)).toBe(TOO_MANY_ATTEMPTS_COPY);
  });

  it('accuses the credentials only where that is what failed', () => {
    expect(authErrorText('invalid_credentials', GENERIC)).toBe(INVALID_SIGN_IN_COPY);
  });

  it('asks an unconfirmed user for the inbox rather than the password', () => {
    expect(authErrorText('email_not_confirmed', GENERIC)).toBe(EMAIL_NOT_CONFIRMED_COPY);
  });

  it('falls back to the caller-supplied copy for every other reason', () => {
    const others: AuthErrorReason[] = ['unknown', 'weak_password', 'already_registered'];

    expect(others.map((reason) => authErrorText(reason, GENERIC))).toEqual(
      others.map(() => GENERIC),
    );
  });
});
