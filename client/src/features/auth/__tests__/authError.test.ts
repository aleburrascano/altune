import {
  authErrorText,
  classifyAuthError,
  isInvalidCredentialsError,
  isSignupDisabledError,
  isUnconfirmedEmailError,
  type AuthErrorReason,
} from '@features/auth/domain/authError';

const GENERIC = "Couldn't sign you in. Please try again.";
const NETWORK_ERROR_COPY = "Can't reach the server. Check your connection and try again.";
const INVALID_SIGN_IN_COPY = 'Email or password is incorrect.';
const EMAIL_NOT_CONFIRMED_COPY =
  'Confirm your email first. Check your inbox for the confirmation link.';
const TOO_MANY_ATTEMPTS_COPY = 'Too many attempts. Wait a moment and try again.';
const SIGNUP_DISABLED_COPY = 'Sign-ups are closed on staging.';

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

  it('says sign-ups are closed rather than blaming the person', () => {
    expect(authErrorText('signup_disabled', GENERIC)).toBe(SIGNUP_DISABLED_COPY);
    expect(SIGNUP_DISABLED_COPY).toBe('Sign-ups are closed on staging.');
  });

  it('falls back to the caller-supplied copy for every other reason', () => {
    const others: AuthErrorReason[] = ['unknown', 'weak_password', 'already_registered'];

    expect(others.map((reason) => authErrorText(reason, GENERIC))).toEqual(
      others.map(() => GENERIC),
    );
  });
});

describe('classifyAuthError', () => {
  it('reports rate limiting ahead of transport when an error is both', () => {
    expect(classifyAuthError({ status: 429, name: 'AuthRetryableFetchError' })).toBe(
      'too_many_attempts',
    );
  });

  it('reports transport ahead of the action rungs', () => {
    const error = { status: 503, code: 'weak_password' };
    expect(classifyAuthError(error, [[(e) => e.code === 'weak_password', 'weak_password']])).toBe(
      'network',
    );
  });

  it('takes the first matching rung in the order given', () => {
    const error = { code: 'email_not_confirmed' };
    const both = () => true;
    expect(
      classifyAuthError(error, [
        [isInvalidCredentialsError, 'invalid_credentials'],
        [both, 'email_not_confirmed'],
        [isUnconfirmedEmailError, 'weak_password'],
      ]),
    ).toBe('email_not_confirmed');
  });

  it('falls to unknown when nothing matches, with or without rungs', () => {
    expect(classifyAuthError({ status: 400 })).toBe('unknown');
    expect(
      classifyAuthError({ status: 400 }, [[isInvalidCredentialsError, 'invalid_credentials']]),
    ).toBe('unknown');
  });
});

describe('isSignupDisabledError', () => {
  it('recognises the signup_disabled code and nothing else', () => {
    expect(isSignupDisabledError({ status: 422, code: 'signup_disabled' })).toBe(true);
    expect(isSignupDisabledError({ status: 422, code: 'weak_password' })).toBe(false);
  });
});
