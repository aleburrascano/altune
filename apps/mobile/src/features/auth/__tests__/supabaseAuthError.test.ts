import {
  classifyAuthError,
  isInvalidCredentialsError,
  isSignupDisabledError,
  isUnconfirmedEmailError,
} from '../supabaseAuthError';

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
