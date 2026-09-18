export type AuthErrorReason =
  | 'network'
  | 'unknown'
  | 'invalid_credentials'
  | 'weak_password'
  | 'already_registered'
  | 'too_many_attempts';

export const NETWORK_ERROR_COPY = "Can't reach the server. Check your connection and try again.";

// No countdown and no mention of the account: the wait is the same whether the
// address exists or not, and the copy must not say otherwise (see attemptLockout).
export const TOO_MANY_ATTEMPTS_COPY = 'Too many attempts. Wait a moment and try again.';

export function authErrorText(reason: AuthErrorReason, generic: string): string {
  if (reason === 'network') return NETWORK_ERROR_COPY;
  if (reason === 'too_many_attempts') return TOO_MANY_ATTEMPTS_COPY;
  return generic;
}
