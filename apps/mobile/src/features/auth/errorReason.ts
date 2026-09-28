export type AuthErrorReason =
  | 'network'
  | 'unknown'
  | 'invalid_credentials'
  | 'email_not_confirmed'
  | 'weak_password'
  | 'already_registered'
  | 'too_many_attempts';

export const NETWORK_ERROR_COPY = "Can't reach the server. Check your connection and try again.";

export const INVALID_SIGN_IN_COPY = 'Email or password is incorrect.';

export const EMAIL_NOT_CONFIRMED_COPY =
  'Confirm your email first. Check your inbox for the confirmation link.';

export const TOO_MANY_ATTEMPTS_COPY = 'Too many attempts. Wait a moment and try again.';

export function authErrorText(reason: AuthErrorReason, generic: string): string {
  if (reason === 'network') return NETWORK_ERROR_COPY;
  if (reason === 'too_many_attempts') return TOO_MANY_ATTEMPTS_COPY;
  if (reason === 'invalid_credentials') return INVALID_SIGN_IN_COPY;
  if (reason === 'email_not_confirmed') return EMAIL_NOT_CONFIRMED_COPY;
  return generic;
}
