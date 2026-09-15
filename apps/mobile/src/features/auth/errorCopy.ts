export type AuthErrorReason =
  | 'network'
  | 'unknown'
  | 'invalid_credentials'
  | 'weak_password'
  | 'already_registered';

export const NETWORK_ERROR_COPY = "Can't reach the server. Check your connection and try again.";

export function authErrorText(reason: AuthErrorReason, generic: string): string {
  return reason === 'network' ? NETWORK_ERROR_COPY : generic;
}
