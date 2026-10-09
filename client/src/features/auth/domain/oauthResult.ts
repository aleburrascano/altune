import { isNetworkError } from '@shared/failure/errors';

import {
  classifyAuthError,
  type SupabaseAuthErrorLike,
  type TransientAuthReason,
} from '@features/auth/domain/authError';

export type OAuthProvider = 'google';

export type OAuthResult =
  | { kind: 'idle' }
  | { kind: 'pending'; provider: OAuthProvider }
  | { kind: 'ok' }
  | { kind: 'cancelled' }
  | {
      kind: 'error';
      reason: TransientAuthReason;
    };

export type OAuthOutcome = Exclude<OAuthResult, { kind: 'idle' } | { kind: 'pending' }>;
export type OAuthFailure = Extract<OAuthOutcome, { kind: 'error' }>;

export const OAUTH_BROWSER_TIMEOUT_MS = 5 * 60_000;

export function failureReason(error: SupabaseAuthErrorLike): OAuthFailure['reason'] {
  return classifyAuthError(error);
}

export function thrownFailure(err: unknown): OAuthFailure {
  return { kind: 'error', reason: isNetworkError(err) ? 'network' : 'unknown' };
}
