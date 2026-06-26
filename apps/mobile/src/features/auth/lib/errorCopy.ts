/**
 * Auth error copy. A transport failure is NOT an enumeration vector, so it
 * gets its own distinct, actionable message; every credential/account outcome
 * stays generic (AC#5) so the UI never reveals whether an email exists.
 */
import type { SignInResult } from '../hooks/useSignIn';
import type { SignUpResult } from '../hooks/useSignUp';

type ErrorReason =
  | Extract<SignInResult, { kind: 'error' }>['reason']
  | Extract<SignUpResult, { kind: 'error' }>['reason'];

export const NETWORK_ERROR_COPY =
  "Can't reach the server. Check your connection and try again.";

/** Network is surfaced distinctly; all else falls back to the generic copy. */
export function authErrorText(reason: ErrorReason, generic: string): string {
  return reason === 'network' ? NETWORK_ERROR_COPY : generic;
}
