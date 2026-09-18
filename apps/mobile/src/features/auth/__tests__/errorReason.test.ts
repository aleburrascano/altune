import {
  EMAIL_NOT_CONFIRMED_COPY,
  INVALID_CREDENTIALS_COPY,
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

  // The generic copy would tell a locked-out user their password is wrong, which
  // it is not — they would keep retyping a password that was right all along.
  it('explains the local lockout rather than blaming the credentials', () => {
    expect(authErrorText('too_many_attempts', GENERIC)).toBe(TOO_MANY_ATTEMPTS_COPY);
  });

  // Only a rejection GoTrue named may accuse the password, so the accusation
  // lives here rather than in a screen's fallback copy (#1646).
  it('accuses the credentials only where that is what failed', () => {
    expect(authErrorText('invalid_credentials', GENERIC)).toBe(INVALID_CREDENTIALS_COPY);
  });

  // The password was right; sending this user to reset it wastes the one thing
  // they actually need to be told.
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
