import type { SignInResult } from '../hooks/useSignIn';
import type { SignUpResult } from '../hooks/useSignUp';

type ErrorReason =
  | Extract<SignInResult, { kind: 'error' }>['reason']
  | Extract<SignUpResult, { kind: 'error' }>['reason'];

export const NETWORK_ERROR_COPY = "Can't reach the server. Check your connection and try again.";

export function authErrorText(reason: ErrorReason, generic: string): string {
  return reason === 'network' ? NETWORK_ERROR_COPY : generic;
}
